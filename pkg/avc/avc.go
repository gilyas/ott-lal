// Copyright 2019, Chef.  All rights reserved.
// https://github.com/q191201771/lal
//
// Use of this source code is governed by a MIT-style license
// that can be found in the License file.
//
// Author: Chef (191201771@qq.com)

package avc

import (
	"errors"
	"io"

	"github.com/q191201771/naza/pkg/bele"
	"github.com/q191201771/naza/pkg/nazabits"
)

// Annexb:
//   keywords: MPEG-2 transport stream, ElementaryStream(ES),
//   nalu with start code.
//   e.g. ts
//
// AVCC:
//   keywords: AVC1, MPEG-4, extradata, sequence header, AVCDecoderConfigurationRecord
//   nalu with length prefix.
//   e.g. rtmp, flv

var ErrAvc = errors.New("lal.avc: fxxk")

var (
	NaluStartCode3 = []byte{0x0, 0x0, 0x1}
	NaluStartCode4 = []byte{0x0, 0x0, 0x0, 0x1}

	// aud nalu
	AudNalu = []byte{0x00, 0x00, 0x00, 0x01, 0x09, 0xf0}
)

// H.264-AVC-ISO_IEC_14496-15.pdf
// Table 1 - NAL unit types in elementary streams
var NaluTypeMapping = map[uint8]string{
	1:  "SLICE",
	5:  "IDR",
	6:  "SEI",
	7:  "SPS",
	8:  "PPS",
	9:  "AUD",
	12: "FD",
}

var SliceTypeMapping = map[uint8]string{
	0: "P",
	1: "B",
	2: "I",
	3: "SP",
	4: "SI",
	5: "P",
	6: "B",
	7: "I",
	8: "SP",
	9: "SI",
}

const (
	NaluTypeSlice    uint8 = 1
	NaluTypeIdrSlice uint8 = 5
	NaluTypeSei      uint8 = 6
	NaluTypeSps      uint8 = 7
	NaluTypePps      uint8 = 8
	NaluTypeAud      uint8 = 9  // Access Unit Delimiter
	NaluTypeFd       uint8 = 12 // Filler Data
)

const (
	SliceTypeP  uint8 = 0
	SliceTypeB  uint8 = 1
	SliceTypeI  uint8 = 2
	SliceTypeSp uint8 = 3
	SliceTypeSi uint8 = 4
)

type Context struct {
	Profile uint8
	Level   uint8
	Width   uint32
	Height  uint32
}

// H.264-AVC-ISO_IEC_14496-15.pdf
// 5.2.4 Decoder configuration information
type DecoderConfigurationRecord struct {
	ConfigurationVersion uint8
	AvcProfileIndication uint8
	ProfileCompatibility uint8
	AvcLevelIndication   uint8
	LengthSizeMinusOne   uint8
	NumOfSps             uint8
	SpsLength            uint16
	NumOfPps             uint8
	PpsLength            uint16
}

// ISO-14496-10.pdf
// 7.3.2.1 Sequence parameter set RBSP syntax
// 7.4.2.1 Sequence parameter set RBSP semantics
type Sps struct {
	ProfileIdc                     uint8
	ConstraintSet0Flag             uint8
	ConstraintSet1Flag             uint8
	ConstraintSet2Flag             uint8
	LevelIdc                       uint8
	SpsId                          uint32
	ChromaFormatIdc                uint32
	ResidualColorTransformFlag     uint8
	BitDepthLuma                   uint32
	BitDepthChroma                 uint32
	TransFormBypass                uint8
	Log2MaxFrameNumMinus4          uint32
	PicOrderCntType                uint32
	Log2MaxPicOrderCntLsb          uint32
	NumRefFrames                   uint32 // num_ref_frames
	GapsInFrameNumValueAllowedFlag uint8  // gaps_in_frame_num_value_allowed_flag
	PicWidthInMbsMinusOne          uint32 // pic_width_in_mbs_minus1
	PicHeightInMapUnitsMinusOne    uint32 // pic_height_in_map_units_minus1
	FrameMbsOnlyFlag               uint8  // frame_mbs_only_flag
	MbAdaptiveFrameFieldFlag       uint8  // mb_adaptive_frame_field_flag
	Direct8X8InferenceFlag         uint8  // direct_8x8_inference_flag
	FrameCroppingFlag              uint8  // frame_cropping_flag
	FrameCropLeftOffset            uint32 // frame_crop_left_offset
	FrameCropRightOffset           uint32 // frame_crop_right_offset
	FrameCropTopOffset             uint32 // frame_crop_top_offset
	FrameCropBottomOffset          uint32 // frame_crop_bottom_offset
}

func ParseNaluType(v uint8) uint8 {
	return v & 0x1f
}

func ParseSliceType(nalu []byte) (uint8, error) {
	if len(nalu) < 2 {
		return 0, ErrAvc
	}

	br := nazabits.NewBitReader(nalu[1:])

	// skip first_mb_in_slice
	if _, err := br.ReadGolomb(); err != nil {
		return 0, err
	}

	sliceType, err := br.ReadGolomb()
	if err != nil {
		return 0, err
	}

	// range: [0, 9]
	if sliceType > 9 {
		return 0, ErrAvc
	}

	if sliceType > 4 {
		sliceType -= 5
	}
	return uint8(sliceType), nil
}

func ParseNaluTypeReadable(v uint8) string {
	t := ParseNaluType(v)
	ret, ok := NaluTypeMapping[t]
	if !ok {
		return "unknown"
	}
	return ret
}

func ParseSliceTypeReadable(nalu []byte) (string, error) {
	naluType := ParseNaluType(nalu[0])

	// ???????????????????????????????????????????????????slice type
	switch naluType {
	case NaluTypeSei:
		fallthrough
	case NaluTypeSps:
		fallthrough
	case NaluTypePps:
		return "", nil
	}

	t, err := ParseSliceType(nalu)
	if err != nil {
		return "unknown", err
	}
	ret, ok := SliceTypeMapping[t]
	if !ok {
		return "unknown", ErrAvc
	}
	return ret, nil
}

// AVCC Seq Header -> Annexb
//
// @param payload: rtmp message???payload????????????flv tag???payload??????
//                 ????????????????????????2??????????????????3?????????cts
//
// @return ??????????????????????????????????????????
//
func SpsPpsSeqHeader2Annexb(payload []byte) ([]byte, error) {
	sps, pps, err := ParseSpsPpsFromSeqHeaderWithoutMalloc(payload)
	if err != nil {
		return nil, ErrAvc
	}
	var ret []byte
	ret = append(ret, NaluStartCode4...)
	ret = append(ret, sps...)
	ret = append(ret, NaluStartCode4...)
	ret = append(ret, pps...)
	return ret, nil
}

// ???func ParseSpsPpsFromSeqHeaderWithoutMalloc
//
// @return sps, pps: ?????????????????????????????????
//
func ParseSpsPpsFromSeqHeader(payload []byte) (sps, pps []byte, err error) {
	s, p, e := ParseSpsPpsFromSeqHeaderWithoutMalloc(payload)
	if e != nil {
		return nil, nil, e
	}
	sps = append(sps, s...)
	pps = append(pps, p...)
	return
}

// ???AVCC?????????Seq Header?????????SPS???PPS?????????
//
// @param payload: rtmp message???payload????????????flv tag???payload??????
//                 ????????????????????????2??????????????????3?????????cts
//
// @return sps, pps: ??????????????????`payload`????????????
//
func ParseSpsPpsFromSeqHeaderWithoutMalloc(payload []byte) (sps, pps []byte, err error) {
	if len(payload) < 5 {
		return nil, nil, ErrAvc
	}
	if payload[0] != 0x17 || payload[1] != 0x00 || payload[2] != 0 || payload[3] != 0 || payload[4] != 0 {
		return nil, nil, ErrAvc
	}

	if len(payload) < 13 {
		return nil, nil, ErrAvc
	}

	index := 10
	numOfSps := int(payload[index] & 0x1F)
	index++
	if numOfSps != 1 {
		return nil, nil, ErrAvc
	}
	spsLength := int(bele.BeUint16(payload[index:]))
	index += 2

	if len(payload) < 13+spsLength {
		return nil, nil, ErrAvc
	}

	sps = payload[index : index+spsLength]
	index += spsLength

	if len(payload) < 16+spsLength {
		return nil, nil, ErrAvc
	}

	numOfPps := int(payload[index] & 0x1F)
	index++
	if numOfPps != 1 {
		return nil, nil, ErrAvc
	}
	ppsLength := int(bele.BeUint16(payload[index:]))
	index += 2

	if len(payload) < 16+spsLength+ppsLength {
		return nil, nil, ErrAvc
	}

	pps = payload[index : index+ppsLength]
	return
}

// @return ?????????????????????????????????
//
func BuildSeqHeaderFromSpsPps(sps, pps []byte) ([]byte, error) {
	var sh []byte
	sh = make([]byte, 16+len(sps)+len(pps))
	sh[0] = 0x17
	sh[1] = 0x0
	sh[2] = 0x0
	sh[3] = 0x0
	sh[4] = 0x0

	// H.264-AVC-ISO_IEC_14496-15.pdf
	// 5.2.4 Decoder configuration information
	sh[5] = 0x1 // configurationVersion

	var ctx Context
	if err := ParseSps(sps, &ctx); err != nil {
		return nil, err
	}

	sh[6] = ctx.Profile // AvcProfileIndication
	sh[7] = 0           // profile_compatibility
	sh[8] = ctx.Level   // AvcLevelIndication
	sh[9] = 0xFF        // lengthSizeMinusOne '111111'b | (4-1)
	sh[10] = 0xE1       // numOfSequenceParameterSets '111'b | 1

	sh[11] = uint8((len(sps) >> 8) & 0xFF) // sequenceParameterSetLength
	sh[12] = uint8(len(sps) & 0xFF)

	i := 13
	copy(sh[i:], sps)
	i += len(sps)

	sh[i] = 0x1 // numOfPictureParameterSets 1
	i++

	sh[i] = uint8((len(pps) >> 8) & 0xFF) // sequenceParameterSetLength
	sh[i+1] = uint8(len(pps) & 0xFF)
	i += 2

	copy(sh[i:], pps)

	return sh, nil
}

// AVCC -> Annexb
//
// @param payload: rtmp message???payload????????????flv tag???payload??????
//                 ????????????????????????2??????????????????3?????????cts
//
func CaptureAvcc2Annexb(w io.Writer, payload []byte) error {
	// sps pps
	if payload[0] == 0x17 && payload[1] == 0x00 {
		spspps, err := SpsPpsSeqHeader2Annexb(payload)
		if err != nil {
			return err
		}
		_, _ = w.Write(spspps)
		return nil
	}

	// TODO(chef): [refactor] ??????IterateNaluAvcc
	// payload?????????????????????nalu
	for i := 5; i != len(payload); {
		naluLen := int(bele.BeUint32(payload[i:]))
		i += 4
		_, _ = w.Write(NaluStartCode4)
		_, _ = w.Write(payload[i : i+naluLen])
		i += naluLen
		break
	}
	return nil
}

// ???????????????????????????nalu start code?????????
//
// @param start: ???`nalu`???start??????????????????
//
// @return pos:    start code????????????????????????start code?????????
//         length: start code?????????????????????3??????4
//         ????????????????????????start code????????????-1, -1
//
func IterateNaluStartCode(nalu []byte, start int) (pos, length int) {
	if nalu == nil || start >= len(nalu) {
		return -1, -1
	}
	count := 0
	for i := range nalu[start:] {
		switch nalu[start+i] {
		case 0:
			count++
		case 1:
			if count >= 2 {
				return start + i - count, count + 1
			}
			count = 0
		default:
			count = 0
		}
	}
	return -1, -1
}

// ??????Annexb???????????????start code?????????nal??????????????????????????????1???????????????????????????????????????????????????
//
// ?????????????????????
//
func SplitNaluAnnexb(nals []byte) (nalList [][]byte, err error) {
	err = IterateNaluAnnexb(nals, func(nal []byte) {
		nalList = append(nalList, nal)
	})
	return
}

// ??????AVCC???????????????4?????????????????????nal?????????????????????????????????1???????????????????????????????????????????????????
//
// ?????????????????????
//
func SplitNaluAvcc(nals []byte) (nalList [][]byte, err error) {
	err = IterateNaluAvcc(nals, func(nal []byte) {
		nalList = append(nalList, nal)
	})
	return

}

func IterateNaluAnnexb(nals []byte, handler func(nal []byte)) error {
	if nals == nil {
		return ErrAvc
	}
	prePos, preLength := IterateNaluStartCode(nals, 0)
	if prePos == -1 {
		handler(nals)
		return ErrAvc
	}

	for {
		start := prePos + preLength
		pos, length := IterateNaluStartCode(nals, start)
		if pos == -1 {
			if start < len(nals) {
				handler(nals[start:])
				return nil
			} else {
				return ErrAvc
			}
		}
		if start < pos {
			handler(nals[start:pos])
		} else {
			return ErrAvc
		}

		prePos = pos
		preLength = length
	}
}

func IterateNaluAvcc(nals []byte, handler func(nal []byte)) error {
	if nals == nil {
		return ErrAvc
	}
	pos := 0
	for {
		if len(nals[pos:]) < 4 {
			return ErrAvc
		}
		length := int(bele.BeUint32(nals[pos:]))
		pos += 4
		if pos == len(nals) {
			return ErrAvc
		}
		epos := pos + length
		if epos < len(nals) {
			// ???????????????
			handler(nals[pos:epos])
			pos += length
		} else if epos == len(nals) {
			// ????????????
			handler(nals[pos:epos])
			return nil
		} else {
			handler(nals[pos:])
			return ErrAvc
		}
	}
}

// TODO(chef): ???????????? func NaluAvcc2Annexb, func NaluAnnexb2Avcc
