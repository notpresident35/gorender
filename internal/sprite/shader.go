package sprite

import (
	"github.com/mattkimber/gorender/internal/colour"
	"github.com/mattkimber/gorender/internal/manifest"
	"github.com/mattkimber/gorender/internal/raycaster"
	"log"
	"math"
)

type ShaderInfo struct {
	Colour         colour.RGB
	SpecialColour  colour.RGB
	Alpha          float64
	Specialness    float64
	Normal         colour.RGB
	AveragedNormal colour.RGB
	Depth          colour.RGB
	Occlusion      colour.RGB
	Lighting       colour.RGB
	Shadowing      colour.RGB
	Detail         colour.RGB
	Transparency   colour.RGB
	Region         int
	ModalIndex     byte
	DitheredIndex  byte
	IsMaskColour   bool
	IsAnimated     bool
}

type ShaderOutput [][]ShaderInfo

type RegionInfo struct {
	ModalCount map[byte]int
	AverageColour colour.RGB
	Distance ColourDistance
	MinIndex byte
	MaxIndex byte
	Size int
	SizeInRange int
	Range *colour.PaletteRange
}

type ColourDistance struct {
	Low float64
	High float64
}

func (cd *ColourDistance) MultiplyColours(midpoint, c colour.RGB) colour.RGB {
	if c.R + c.G + c.B < midpoint.R + midpoint.G + midpoint.B {
		return colour.ClampRGB(colour.RGB{
			R: midpoint.R - ((midpoint.R - c.R) * cd.Low),
			G: midpoint.G - ((midpoint.G - c.G) * cd.Low),
			B: midpoint.B - ((midpoint.B - c.B) * cd.Low),
		})
	}

	return colour.ClampRGB(colour.RGB{
		R: midpoint.R + ((c.R - midpoint.R) * cd.High),
		G: midpoint.G + ((c.G - midpoint.G) * cd.High),
		B: midpoint.B + ((c.B - midpoint.B) * cd.High),
	})
}

func GetColour(s *ShaderInfo) colour.RGB {
	return s.Colour
}

func GetNormal(s *ShaderInfo) colour.RGB {
	return s.Normal
}

func GetAveragedNormal(s *ShaderInfo) colour.RGB {
	return s.AveragedNormal
}

func GetDepth(s *ShaderInfo) colour.RGB {
	return s.Depth
}

func GetOcclusion(s *ShaderInfo) colour.RGB {
	return s.Occlusion
}

func GetLighting(s *ShaderInfo) colour.RGB {
	return s.Lighting
}

func GetShadowing(s *ShaderInfo) colour.RGB {
	return s.Shadowing
}

func GetDetail(s *ShaderInfo) colour.RGB {
	return s.Detail
}

func GetTransparency(s *ShaderInfo) colour.RGB {
	return s.Transparency
}

func GetIndex(s *ShaderInfo) byte {
	return s.DitheredIndex
}

func GetMaskIndex(s *ShaderInfo) byte {
	if s.Specialness > 0.75 || s.IsAnimated {
		return s.ModalIndex
	} else if s.Specialness > 0.25 && s.IsMaskColour {
		return s.DitheredIndex
	}
	return 0
}

func GetRegion(s *ShaderInfo)  colour.RGB {
	return colour.RGB{
		R: float64(s.Region % 4 * (65535/4)),
		G: float64((s.Region/4) % 4 * (65535/4)),
		B: float64((s.Region/16) % 4 * (65535/4)),
	}
}

func GetShaderOutput(renderOutput raycaster.RenderOutput, spr manifest.Sprite, def manifest.Definition, width int, height int) (output ShaderOutput) {
	output = make([][]ShaderInfo, width)

	xoffset, yoffset := int(spr.OffsetX*def.Scale), int(spr.OffsetY*def.Scale)

	// Palettes
	regularPalette := def.Palette.GetRegularPalette()
	primaryCCPalette := def.Palette.GetPrimaryCompanyColourPalette()
	secondaryCCPalette := def.Palette.GetSecondaryCompanyColourPalette()


	prevIndex := byte(0)

	for x := 0; x < width; x++ {
		output[x] = make([]ShaderInfo, height)

		for y := 0; y < height; y++ {
			rx := x + xoffset
			ry := y + yoffset
			if rx < 0 || rx >= width || ry < 0 || ry >= height {
				continue
			}

			if x > 1 {
				prevIndex = output[x-1][y].ModalIndex
			} else {
				prevIndex = 0
			}

			output[x][y] = shade(renderOutput[rx][ry], def, prevIndex)

		}
	}

	currentRegion := 1
	regions := make(map[int]RegionInfo)

	// Calculate regions from the shaded output
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			info := RegionInfo{}

			// No region for transparent/empty voxels
			if output[x][y].ModalIndex == 0 {
				continue
			}

			// Don't set region if it was already set
			if output[x][y].Region != 0 {
				continue
			}

			// Flood fill the region connected to this pixel
			paletteRange := def.Palette.Entries[output[x][y].ModalIndex].Range
			info.Range = paletteRange
			info.ModalCount = make(map[byte]int)

			floodFill(&output, currentRegion, x, y, width, height, &def.Palette, paletteRange)

			regions[currentRegion] = info
			currentRegion++
		}
	}


	// Floyd-Steinberg error rows
	errCurr := make([]colour.RGB, height+2)
	errNext := make([]colour.RGB, height+2)

	var error colour.RGB

	// Get the first pass dithered output to find what the colour ranges are
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {

			bestIndex := ditherOutput(def, output, x, y, error, errCurr, primaryCCPalette, secondaryCCPalette, regularPalette, errNext)

			// Update the range stats
			ditheredRange := def.Palette.Entries[bestIndex].Range
			info := regions[output[x][y].Region]
			info.Size++

			if ditheredRange == info.Range && bestIndex != 0 {

				info.SizeInRange++
				col := output[x][y].Colour

				if def.Palette.IsSpecialColour(output[x][y].ModalIndex) {
					col = output[x][y].SpecialColour
				}

				// Update the "average" in-range colour of this section
				r := ((info.AverageColour.R * (float64(info.SizeInRange) - 1)) + col.R) / float64(info.SizeInRange)
				g := ((info.AverageColour.G * (float64(info.SizeInRange) - 1)) + col.G) / float64(info.SizeInRange)
				b := ((info.AverageColour.B * (float64(info.SizeInRange) - 1)) + col.B) / float64(info.SizeInRange)
				info.AverageColour = colour.RGB{R: r, G: g, B: b}

				if bestIndex < info.MinIndex || info.MinIndex == 0 {
					info.MinIndex = bestIndex
				}

				if bestIndex > info.MaxIndex || info.MaxIndex == 0 {
					info.MaxIndex = bestIndex
				}

				if ct, ok := info.ModalCount[bestIndex]; !ok {
					info.ModalCount[bestIndex] = 1
				} else {
					info.ModalCount[bestIndex] = ct + 1
				}

				regions[output[x][y].Region] = info
			}
		}

		// Swap the next and current error lines
		errCurr, errNext = errNext, errCurr
	}

	for idx, region := range regions {
		if region.Size > 1 {
			log.Printf("region %d: size %d (in range %d) min %d max %d (%d/%d)", idx, region.Size, region.SizeInRange, region.MinIndex, region.MaxIndex, region.Range.Start, region.Range.End)
			log.Printf(" - avg colour: %.0f %.0f %.0f", region.AverageColour.R, region.AverageColour.G, region.AverageColour.B)
			minColour := def.Palette.Entries[region.MinIndex].GetRGB()
			maxColour := def.Palette.Entries[region.MaxIndex].GetRGB()
			log.Printf(" - min colour: %.0f %.0f %.0f", minColour.R, minColour.G, minColour.B)
			log.Printf(" - max colour: %.0f %.0f %.0f", maxColour.R, maxColour.G, maxColour.B)

			lowIndex, highIndex := region.MinIndex, region.MaxIndex

			// Get the new high and low indexes (expand by up to 3 - TODO: should this be configurable in the definition?)
			if region.Range.Start < region.MinIndex {
				if region.MinIndex - region.Range.Start > 3 {
					lowIndex = region.MinIndex - 3
				} else {
					lowIndex = region.Range.Start
				}
			}

			if region.Range.End > region.MaxIndex {
				if region.Range.End - region.MaxIndex > 3 {
					highIndex = region.MaxIndex + 3
				} else {
					highIndex = region.Range.End
				}
			}

			lowColour := def.Palette.Entries[lowIndex].GetRGB()
			highColour := def.Palette.Entries[highIndex].GetRGB()

			distance := ColourDistance{
				Low: ((region.AverageColour.R - lowColour.R) / (region.AverageColour.R - minColour.R) +
					(region.AverageColour.G - lowColour.G) / (region.AverageColour.G - minColour.G) +
					(region.AverageColour.B - lowColour.B) / (region.AverageColour.B - minColour.B)) / 3,
				High: ((region.AverageColour.R - highColour.R) / (region.AverageColour.R - maxColour.R) +
					(region.AverageColour.G - highColour.G) / (region.AverageColour.G - maxColour.G) +
					(region.AverageColour.B - highColour.B) / (region.AverageColour.B - maxColour.B)) / 3,
				}

			region.Distance = distance
			regions[idx] = region

			log.Printf(" - distance: %+v", distance)

		}
	}


	// Do the second pass dithered output to expand the colour range

	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			info := regions[output[x][y].Region]
			prev := output[x][y].Colour
			if info.Size > 1 {
				if def.Palette.IsSpecialColour(output[x][y].ModalIndex) {
					output[x][y].SpecialColour = info.Distance.MultiplyColours(info.AverageColour, output[x][y].SpecialColour)
					log.Printf("went from %+v to %+v", prev, output[x][y].SpecialColour)
				} else {
					output[x][y].Colour = info.Distance.MultiplyColours(info.AverageColour, output[x][y].Colour)
					log.Printf("went from %+v to %+v", prev, output[x][y].Colour)
				}

			}

			ditherOutput(def, output, x, y, error, errCurr, primaryCCPalette, secondaryCCPalette, regularPalette, errNext)
		}

		// Swap the next and current error lines
		errCurr, errNext = errNext, errCurr
	}

	return
}

func ditherOutput(def manifest.Definition, output ShaderOutput, x int, y int, error colour.RGB, errCurr []colour.RGB, primaryCCPalette []colour.RGB, secondaryCCPalette []colour.RGB, regularPalette []colour.RGB, errNext []colour.RGB) byte {
	bestIndex := byte(0)

	rng := def.Palette.Entries[output[x][y].ModalIndex].Range
	if rng == nil {
		rng = &colour.PaletteRange{}
	}

	if output[x][y].Alpha < def.Manifest.EdgeThreshold {
		bestIndex = 0
	} else if rng.IsPrimaryCompanyColour {
		if y > 0 && def.Palette.IsSpecialColour(output[x][y-1].ModalIndex) {
			error = output[x][y].SpecialColour
		} else {
			error = output[x][y].SpecialColour.Add(errCurr[y+1])
		}
		bestIndex = getBestIndex(error, primaryCCPalette)
	} else if rng.IsSecondaryCompanyColour {
		if y > 0 && def.Palette.IsSpecialColour(output[x][y-1].ModalIndex) {
			error = output[x][y].SpecialColour
		} else {
			error = output[x][y].SpecialColour.Add(errCurr[y+1])
		}
		bestIndex = getBestIndex(error, secondaryCCPalette)
	} else if rng.IsAnimatedLight {
		output[x][y].IsAnimated = true
		// Never add error values to special colours
		bestIndex = output[x][y].ModalIndex
		error = def.Palette.Entries[bestIndex].GetRGB()
	} else {
		if y > 0 && def.Palette.IsSpecialColour(output[x][y-1].ModalIndex) {
			error = output[x][y].Colour
		} else {
			error = output[x][y].Colour.Add(errCurr[y+1])
		}
		bestIndex = getBestIndex(error, regularPalette)
	}

	output[x][y].DitheredIndex = bestIndex

	if def.Palette.IsSpecialColour(bestIndex) {
		output[x][y].IsMaskColour = true
	}

	if output[x][y].Alpha >= def.Manifest.EdgeThreshold {
		error = colour.ClampRGB(error.Subtract(def.Palette.Entries[bestIndex].GetRGB()))
	} else {
		error = colour.RGB{}
	}

	// Apply Floyd-Steinberg error
	errNext[y+0] = errNext[y+0].Add(error.MultiplyBy(3.0 / 16))
	errNext[y+1] = errNext[y+1].Add(error.MultiplyBy(5.0 / 16))
	errNext[y+2] = errNext[y+2].Add(error.MultiplyBy(1.0 / 16))
	errCurr[y+2] = errCurr[y+2].Add(error.MultiplyBy(7.0 / 16))

	errCurr[y+1] = colour.RGB{}
	return bestIndex
}

func floodFill(output *ShaderOutput, region int, x, y int, width, height int, palette *colour.Palette, paletteRange *colour.PaletteRange) bool {
	index := (*output)[x][y].ModalIndex
	thisRegion := (*output)[x][y].Region
	thisRange := (*palette).Entries[index].Range

	// If not the same palette range, or we already set the region, return
	if thisRange != paletteRange || thisRegion == region {
		return false
	}

	(*output)[x][y].Region = region

	// Recursively flood fill in the adjacent directions
	if x > 0 {
		floodFill(output, region, x - 1, y, width, height, palette, paletteRange)
	}

	if y > 0 {
		floodFill(output, region, x, y - 1, width, height, palette, paletteRange)
	}

	if x < width - 1 {
		floodFill(output, region, x + 1, y, width, height, palette, paletteRange)
	}

	if y < height - 1 {
		floodFill(output, region, x, y + 1, width, height, palette, paletteRange)
	}

	return true
}

func getBestIndex(error colour.RGB, palette []colour.RGB) byte {
	bestIndex, bestSum := 0, math.MaxFloat64
	for index, p := range palette {
		if p.R > 65000 && (p.G == 0 || p.G > 65000) && p.B > 65000 {
			continue
		}

		sum := squareDiff(error.R, p.R) + squareDiff(error.G, p.G) + squareDiff(error.B, p.B)
		if sum < bestSum {
			bestIndex, bestSum = index, sum
			if sum == 0 {
				break
			}
		}
	}

	return byte(bestIndex)
}

func squareDiff(a, b float64) float64 {
	diff := a - b
	return diff * diff
}

func shade(info raycaster.RenderInfo, def manifest.Definition, prevIndex byte) (output ShaderInfo) {
	totalInfluence, filledInfluence := 0.0, 0.0
	filledSamples, totalSamples := 0, 0
	values := map[byte]float64{}
	fAccuracy := float64(def.Manifest.Accuracy)
	hardEdgeThreshold := int(def.Manifest.HardEdgeThreshold * 100.0)

	minDepth := math.MaxInt64
	for _, s := range info {
		if s.Collision && s.Depth < minDepth {
			minDepth = s.Depth
		}
	}

	for _, s := range info {
		if s.IsRecovered {
			s.Influence = s.Influence * (1.0 - def.Manifest.RecoveredVoxelSuppression)
		}

		// Voxel samples considered to be more representative of fine details can be boosted
		// to make them more likely to appear in the output.
		if def.Manifest.DetailBoost != 0 {
			s.Influence = s.Influence * (1.0 + (s.Detail * def.Manifest.DetailBoost))
		}

		// Boost samples closest to the camera
		if s.Depth != minDepth {
			s.Influence = s.Influence / fAccuracy
		}

		totalInfluence += s.Influence

		if s.Collision && def.Palette.IsRenderable(s.Index) {
			filledInfluence += s.Influence
			filledSamples += s.Count

			output.Colour = output.Colour.Add(Colour(s, def, true, s.Influence))
			output.SpecialColour = output.SpecialColour.Add(Colour(s, def, false, s.Influence))

			if def.Palette.IsSpecialColour(s.Index) {
				output.Specialness += 1.0 * s.Influence
				values[s.Index]++
			}

			if s.Index != 0 {
				values[s.Index] += s.Influence
			}

			if def.Debug {
				// Loop makes this a little slower but is fine for debug purposes
				for i := 0; i < s.Count; i++ {
					output.Normal = output.Normal.Add(Normal(s))
					output.AveragedNormal = output.AveragedNormal.Add(AveragedNormal(s))
					output.Depth = output.Depth.Add(Depth(s))
					output.Occlusion = output.Occlusion.Add(Occlusion(s))
					output.Shadowing = output.Shadowing.Add(Shadow(s))
					output.Lighting = output.Lighting.Add(Lighting(s))
					output.Detail = output.Detail.Add(Detail(s))
				}
			}
		}

		totalSamples = totalSamples + s.Count
	}

	max := 0.0
	alternateModal := byte(0)

	for k, v := range values {
		if v > max {
			max = v
			// Store the previous modal
			alternateModal = output.ModalIndex
			output.ModalIndex = k
		}
	}

	// Supply a same-range alternative if we are going to repeat the same colour and we have an alternative
	if output.ModalIndex == prevIndex && def.Palette.Entries[output.ModalIndex].Range == def.Palette.Entries[alternateModal].Range && alternateModal != 0 {
		output.ModalIndex = alternateModal
	}

	// Fewer than hard edge threshold collisions = transparent
	if totalSamples == 0 || filledSamples * 100 / totalSamples <= hardEdgeThreshold {
		return ShaderInfo{}
	}

	// Soften edges means that when only some rays collided (typically near edges
	// of an object) we fade to transparent. Otherwise objects are hard-edged, which
	// makes them more likely to suffer aliasing artifacts but also clearer at small
	// sizes
	output.Alpha = 1.0
	divisor := filledInfluence

	if def.SoftenEdges() {
		output.Alpha = divisor / totalInfluence
	}

	if def.Manifest.FadeToBlack {
		divisor = totalInfluence
	}

	output.Colour.DivideAndClamp(divisor)
	output.SpecialColour.DivideAndClamp(divisor)

	output.Specialness = output.Specialness / divisor

	if def.Debug {
		debugDivisor := float64(filledSamples)
		output.Normal.DivideAndClamp(debugDivisor)
		output.AveragedNormal.DivideAndClamp(debugDivisor)
		output.Depth.DivideAndClamp(debugDivisor)
		output.Occlusion.DivideAndClamp(debugDivisor)
		output.Shadowing.DivideAndClamp(debugDivisor)
		output.Lighting.DivideAndClamp(debugDivisor)
		output.Detail.DivideAndClamp(debugDivisor)
		output.Transparency = FloatValue(float64(filledSamples)/ float64(totalSamples))
	}

	return
}
