// Audio and Video utils

package main

/* Consts */

var AUDIO_CODEC_NAME = []string{
	"",
	"ADPCM",
	"MP3",
	"LinearLE",
	"Nellymoser16",
	"Nellymoser8",
	"Nellymoser",
	"G711A",
	"G711U",
	"",
	"AAC",
	"Speex",
	"",
	"OPUS",
	"MP3-8K",
	"DeviceSpecific",
	"Uncompressed",
}

var AUDIO_SOUND_RATE = []uint32{
	5512, 11025, 22050, 44100,
}

var VIDEO_CODEC_NAME = []string{
	"",
	"Jpeg",
	"Sorenson-H263",
	"ScreenVideo",
	"On2-VP6",
	"On2-VP6-Alpha",
	"ScreenVideo2",
	"H264",
	"",
	"",
	"",
	"",
	"H265",
}

/* AAC (Advanced Audio Coding) */

var AAC_SAMPLE_RATE = []uint32{
	96000, 88200, 64000, 48000,
	44100, 32000, 24000, 22050,
	16000, 12000, 11025, 8000,
	7350, 0, 0, 0,
}

var AAC_CHANNELS = []uint32{
	0, 1, 2, 3, 4, 5, 6, 8,
}

type AACSpecificConfig struct {
	object_type     uint32
	sample_rate     uint32
	sampling_index  byte
	chan_config     uint32
	channels        uint32
	sbr             int32
	ps              int32
	ext_object_type uint32
}

func getAudioObjectType(bitop Bitop) uint32 {
	var r uint32
	r = bitop.Read(5)
	if r == 31 {
		r = bitop.Read(6) + 32
	}
	return r
}

func getAudioSampleRate(bitop Bitop, sampling_index byte) uint32 {
	if sampling_index == 0x0f {
		return bitop.Read(24)
	} else if int(sampling_index) < len(AAC_SAMPLE_RATE) {
		return AAC_SAMPLE_RATE[sampling_index]
	} else {
		return 0
	}
}

func readAACSpecificConfig(aacSequenceHeader []byte) AACSpecificConfig {
	res := AACSpecificConfig{
		object_type:     0,
		sample_rate:     0,
		sampling_index:  0,
		chan_config:     0,
		channels:        0,
		sbr:             0,
		ps:              0,
		ext_object_type: 0,
	}
	bitop := createBitop(aacSequenceHeader)

	bitop.Read(16)

	res.object_type = getAudioObjectType(bitop)
	res.sampling_index = byte(bitop.Read(4))
	res.sample_rate = getAudioSampleRate(bitop, res.sampling_index)
	res.chan_config = bitop.Read(4)

	if int(res.chan_config) < len(AAC_CHANNELS) {
		res.channels = AAC_CHANNELS[res.chan_config]
	}

	res.sbr = -1
	res.ps = -1

	if res.object_type == 5 || res.object_type == 29 {
		if res.object_type == 29 {
			res.ps = 1
		}
		res.ext_object_type = 5
		res.sbr = 1
		res.sampling_index = byte(bitop.Read(4))
		res.sample_rate = getAudioSampleRate(bitop, res.sampling_index)
		res.object_type = getAudioObjectType(bitop)
	}

	return res
}

func getAACProfileName(info AACSpecificConfig) string {
	switch info.object_type {
	case 1:
		return "Main"
	case 2:
		if info.ps > 0 {
			return "HEv2"
		}
		if info.sbr > 0 {
			return "HE"
		}
		return "LC"
	case 3:
		return "SSR"
	case 4:
		return "LTP"
	case 5:
		return "SBR"
	default:
		return ""
	}
}

/* H264 Video Codec */

type H264SpecificConfig struct {
	width          uint32
	height         uint32
	profile        byte
	compat         byte
	level          float32
	nalu           byte
	nb_sps         byte
	avc_ref_frames uint32
}

func readH264SpecificConfig(avcSequenceHeader []byte) H264SpecificConfig {
	res := H264SpecificConfig{
		width:          0,
		height:         0,
		profile:        0,
		compat:         0,
		level:          0,
		nalu:           0,
		nb_sps:         0,
		avc_ref_frames: 0,
	}
	bitop := createBitop(avcSequenceHeader)

	bitop.Read(48)

	res.profile = byte(bitop.Read(8))
	res.compat = byte(bitop.Read(8))
	res.level = float32(bitop.Read(8))

	res.nalu = (byte(bitop.Read(8)) & 0x03) + 1
	res.nb_sps = byte(bitop.Read(8)) & 0x1F

	if res.nb_sps != 0 {
		bitop.Read(16) // Nal size
		nt := bitop.Read(8)

		if nt == 0x67 {
			/* SPS */
			profile_idc := bitop.Read(8)
			bitop.Read(8)      /* Flags */
			bitop.Read(8)      /* Level */
			bitop.ReadGolomb() /* SPS ID */

			if profile_idc == 100 || profile_idc == 110 || profile_idc == 122 || profile_idc == 244 || profile_idc == 44 || profile_idc == 83 || profile_idc == 86 || profile_idc == 118 {
				/* chroma format idc */
				cf_idc := bitop.ReadGolomb()

				if cf_idc == 3 {
					/* separate color plane */
					bitop.Read(1)
				}

				/* bit depth luma - 8 */
				bitop.ReadGolomb()

				/* bit depth chroma - 8 */
				bitop.ReadGolomb()

				/* qpprime y zero transform bypass */
				bitop.Read(1)

				/* seq scaling matrix present */
				ssm := bitop.Read(1)
				if ssm != 0 {
					if cf_idc == 3 {
						bitop.Read(12)
					} else {
						bitop.Read(8)
					}
				}
			}

			/* log2 max frame num */
			bitop.ReadGolomb()

			/* pic order cnt type */
			cnt_type := bitop.ReadGolomb()
			switch cnt_type {
			case 0:
				/* max pic order cnt */
				bitop.ReadGolomb()
			case 1:
				/* delta pic order alwys zero */
				bitop.Read(1)

				/* offset for non-ref pic */
				bitop.ReadGolomb()

				/* offset for top to bottom field */
				bitop.ReadGolomb()

				/* num ref frames in pic order */
				numRefFrames := bitop.ReadGolomb()

				for n := uint32(0); n < numRefFrames; n++ {

					/* offset for ref frame */
					bitop.ReadGolomb()
				}
			}

			/* num ref frames */
			res.avc_ref_frames = bitop.ReadGolomb()

			/* gaps in frame num allowed */
			bitop.Read(1)

			/* pic width in mbs - 1 */
			width := bitop.ReadGolomb()

			/* pic height in map units - 1 */
			height := bitop.ReadGolomb()

			/* frame mbs only flag */
			frame_mbs_only := bitop.Read(1)

			if frame_mbs_only == 0 {

				/* mbs adaprive frame field */
				bitop.Read(1)
			}

			/* direct 8x8 inference flag */
			bitop.Read(1)

			/* frame cropping */

			var crop_left uint32
			var crop_right uint32
			var crop_top uint32
			var crop_bottom uint32

			has_crop := bitop.Read(1)

			if has_crop != 0 {
				crop_left = bitop.ReadGolomb()
				crop_right = bitop.ReadGolomb()
				crop_top = bitop.ReadGolomb()
				crop_bottom = bitop.ReadGolomb()
			} else {
				crop_left = 0
				crop_right = 0
				crop_top = 0
				crop_bottom = 0
			}

			res.level = res.level / 10.0
			res.width = (width+1)*16 - (crop_left+crop_right)*2
			res.height = (2-frame_mbs_only)*(height+1)*16 - (crop_top+crop_bottom)*2
		}
	}

	return res
}

/* HEVC */

type PTL struct {
	profile_space                      uint32
	tier_flag                          uint32
	profile_idc                        uint32
	profile_compatibility_flags        uint32
	general_progressive_source_flag    uint32
	general_interlaced_source_flag     uint32
	general_non_packed_constraint_flag uint32
	general_frame_only_constraint_flag uint32
	level_idc                          uint32

	sub_layer_profile_present_flag       []byte
	sub_layer_level_present_flag         []byte
	sub_layer_profile_space              []byte
	sub_layer_tier_flag                  []byte
	sub_layer_profile_idc                []byte
	sub_layer_profile_compatibility_flag []byte
	sub_layer_progressive_source_flag    []byte
	sub_layer_interlaced_source_flag     []byte
	sub_layer_non_packed_constraint_flag []byte
	sub_layer_frame_only_constraint_flag []byte
	sub_layer_level_idc                  []byte
}

func HEVCParsePtl(bitop Bitop, max_sub_layers_minus1 uint32) PTL {
	general_ptl := PTL{
		profile_space:                      0,
		tier_flag:                          0,
		profile_idc:                        0,
		profile_compatibility_flags:        0,
		general_progressive_source_flag:    0,
		general_interlaced_source_flag:     0,
		general_non_packed_constraint_flag: 0,
		general_frame_only_constraint_flag: 0,
		level_idc:                          0,

		sub_layer_profile_present_flag:       make([]byte, 0),
		sub_layer_level_present_flag:         make([]byte, 0),
		sub_layer_profile_space:              make([]byte, 0),
		sub_layer_tier_flag:                  make([]byte, 0),
		sub_layer_profile_idc:                make([]byte, 0),
		sub_layer_profile_compatibility_flag: make([]byte, 0),
		sub_layer_progressive_source_flag:    make([]byte, 0),
		sub_layer_interlaced_source_flag:     make([]byte, 0),
		sub_layer_non_packed_constraint_flag: make([]byte, 0),
		sub_layer_frame_only_constraint_flag: make([]byte, 0),
		sub_layer_level_idc:                  make([]byte, 0),
	}

	general_ptl.profile_space = bitop.Read(2)
	general_ptl.tier_flag = bitop.Read(1)
	general_ptl.profile_idc = bitop.Read(5)
	general_ptl.profile_compatibility_flags = bitop.Read(32)
	general_ptl.general_progressive_source_flag = bitop.Read(1)
	general_ptl.general_interlaced_source_flag = bitop.Read(1)
	general_ptl.general_non_packed_constraint_flag = bitop.Read(1)
	general_ptl.general_frame_only_constraint_flag = bitop.Read(1)
	bitop.Read(32)
	bitop.Read(12)
	general_ptl.level_idc = bitop.Read(8)

	for i := uint32(0); i < max_sub_layers_minus1; i++ {
		general_ptl.sub_layer_profile_present_flag = append(general_ptl.sub_layer_profile_present_flag, byte(bitop.Read(1)))
		general_ptl.sub_layer_level_present_flag = append(general_ptl.sub_layer_level_present_flag, byte(bitop.Read(1)))
	}

	if max_sub_layers_minus1 > 0 {
		for i := max_sub_layers_minus1; i < 8; i++ {
			bitop.Read(2)
		}
	}

	for i := 0; i < int(max_sub_layers_minus1); i++ {
		if i < len(general_ptl.sub_layer_profile_present_flag) && general_ptl.sub_layer_profile_present_flag[i] != 0 {
			general_ptl.sub_layer_profile_space = append(general_ptl.sub_layer_profile_space, byte(bitop.Read(2)))
			general_ptl.sub_layer_tier_flag = append(general_ptl.sub_layer_tier_flag, byte(bitop.Read(1)))
			general_ptl.sub_layer_profile_idc = append(general_ptl.sub_layer_profile_idc, byte(bitop.Read(5)))
			general_ptl.sub_layer_profile_compatibility_flag = append(general_ptl.sub_layer_profile_compatibility_flag, byte(bitop.Read(32)))
			general_ptl.sub_layer_progressive_source_flag = append(general_ptl.sub_layer_progressive_source_flag, byte(bitop.Read(1)))
			general_ptl.sub_layer_interlaced_source_flag = append(general_ptl.sub_layer_interlaced_source_flag, byte(bitop.Read(1)))
			general_ptl.sub_layer_non_packed_constraint_flag = append(general_ptl.sub_layer_non_packed_constraint_flag, byte(bitop.Read(1)))
			general_ptl.sub_layer_frame_only_constraint_flag = append(general_ptl.sub_layer_frame_only_constraint_flag, byte(bitop.Read(1)))
			bitop.Read(32)
			bitop.Read(12)
		}
		if i < len(general_ptl.sub_layer_level_present_flag) && general_ptl.sub_layer_level_present_flag[i] != 0 {
			general_ptl.sub_layer_level_idc = append(general_ptl.sub_layer_level_idc, byte(bitop.Read(8)))
		} else {
			general_ptl.sub_layer_level_idc = append(general_ptl.sub_layer_level_idc, byte(1))
		}
	}

	return general_ptl
}

type SPS struct {
	profile_tier_level PTL

	sps_video_parameter_set_id   uint32
	sps_max_sub_layers_minus1    uint32
	sps_temporal_id_nesting_flag uint32
	sps_seq_parameter_set_id     uint32
	chroma_format_idc            uint32
	separate_colour_plane_flag   uint32
	pic_width_in_luma_samples    uint32
	pic_height_in_luma_samples   uint32
	conformance_window_flag      uint32
	conf_win_left_offset         uint32
	conf_win_right_offset        uint32
	conf_win_top_offset          uint32
	conf_win_bottom_offset       uint32
}

func HEVCParseSPS(buf []byte) SPS {
	psps := SPS{}
	bitop := createBitop(buf)
	NumBytesInNALunit := len(buf)

	var rbsp_array []byte
	rbsp_array = make([]byte, 0)

	bitop.Read(1) //forbidden_zero_bit
	bitop.Read(6) //nal_unit_type
	bitop.Read(6) //nuh_reserved_zero_6bits
	bitop.Read(3) //nuh_temporal_id_plus1

	for i := 2; i < NumBytesInNALunit; i++ {
		if i+2 < NumBytesInNALunit && bitop.Look(24) == 0x000003 {
			rbsp_array = append(rbsp_array, byte(bitop.Read(8)))
			rbsp_array = append(rbsp_array, byte(bitop.Read(8)))
			i += 2
			bitop.Read(8) /* emulation_prevention_three_byte equal to 0x03 */
		} else {
			rbsp_array = append(rbsp_array, byte(bitop.Read(8)))
		}
	}

	rbspBitop := createBitop(rbsp_array)

	psps.sps_video_parameter_set_id = rbspBitop.Read(4)
	psps.sps_max_sub_layers_minus1 = rbspBitop.Read(3)
	psps.sps_temporal_id_nesting_flag = rbspBitop.Read(1)
	psps.profile_tier_level = HEVCParsePtl(rbspBitop, psps.sps_max_sub_layers_minus1)
	psps.sps_seq_parameter_set_id = rbspBitop.ReadGolomb()
	psps.chroma_format_idc = rbspBitop.ReadGolomb()
	if psps.chroma_format_idc == 3 {
		psps.separate_colour_plane_flag = rbspBitop.Read(1)
	} else {
		psps.separate_colour_plane_flag = 0
	}
	psps.pic_width_in_luma_samples = rbspBitop.ReadGolomb()
	psps.pic_height_in_luma_samples = rbspBitop.ReadGolomb()
	psps.conformance_window_flag = rbspBitop.Read(1)
	if psps.conformance_window_flag != 0 {
		var vert_mult uint32
		var horiz_mult uint32

		if psps.chroma_format_idc < 2 {
			vert_mult = 2
		} else {
			vert_mult = 1
		}

		if psps.chroma_format_idc < 3 {
			horiz_mult = 2
		} else {
			horiz_mult = 1
		}

		psps.conf_win_left_offset = rbspBitop.ReadGolomb() * horiz_mult
		psps.conf_win_right_offset = rbspBitop.ReadGolomb() * horiz_mult
		psps.conf_win_top_offset = rbspBitop.ReadGolomb() * vert_mult
		psps.conf_win_bottom_offset = rbspBitop.ReadGolomb() * vert_mult
	}

	return psps
}

type HEVCSpecificConfig struct {
	width   uint32
	height  uint32
	profile uint32
	level   float32
}

type HEVCMetadata struct {
	configurationVersion uint32

	psps SPS

	general_profile_space               uint32
	general_tier_flag                   uint32
	general_profile_idc                 uint32
	general_profile_compatibility_flags uint32
	general_constraint_indicator_flags  uint32
	general_level_idc                   uint32
	min_spatial_segmentation_idc        uint32
	parallelismType                     uint32
	chromaFormat                        uint32
	bitDepthLumaMinus8                  uint32
	bitDepthChromaMinus8                uint32
	avgFrameRate                        uint32
	constantFrameRate                   uint32
	numTemporalLayers                   uint32
	temporalIdNested                    uint32
	lengthSizeMinusOne                  uint32
}

func readHEVCSpecificConfig(hevcSequenceHeader []byte) HEVCSpecificConfig {
	info := HEVCSpecificConfig{
		width:   0,
		height:  0,
		profile: 0,
		level:   0,
	}

	if len(hevcSequenceHeader) < 5 {
		return info
	}

	hevcSequenceHeader = hevcSequenceHeader[5:]

	if len(hevcSequenceHeader) < 23 {
		return info
	}

	hevc := HEVCMetadata{}

	hevc.configurationVersion = uint32(hevcSequenceHeader[0])
	if hevc.configurationVersion != 1 {
		return info
	}

	hevc.general_profile_space = (uint32(hevcSequenceHeader[1]) >> 6) & 0x03
	hevc.general_tier_flag = (uint32(hevcSequenceHeader[1]) >> 5) & 0x01
	hevc.general_profile_idc = uint32(hevcSequenceHeader[1]) & 0x1F
	hevc.general_profile_compatibility_flags = (uint32(hevcSequenceHeader[2]) << 24) | (uint32(hevcSequenceHeader[3]) << 16) | (uint32(hevcSequenceHeader[4]) << 8) | uint32(hevcSequenceHeader[5])
	hevc.general_constraint_indicator_flags = ((uint32(hevcSequenceHeader[6]) << 24) | (uint32(hevcSequenceHeader[7]) << 16) | (uint32(hevcSequenceHeader[8]) << 8) | uint32(hevcSequenceHeader[9]))
	hevc.general_constraint_indicator_flags = (hevc.general_constraint_indicator_flags << 16) | (uint32(hevcSequenceHeader[10]) << 8) | uint32(hevcSequenceHeader[11])
	hevc.general_level_idc = uint32(hevcSequenceHeader[12])
	hevc.min_spatial_segmentation_idc = ((uint32(hevcSequenceHeader[13]) & 0x0F) << 8) | uint32(hevcSequenceHeader[14])
	hevc.parallelismType = uint32(hevcSequenceHeader[15]) & 0x03
	hevc.chromaFormat = uint32(hevcSequenceHeader[16]) & 0x03
	hevc.bitDepthLumaMinus8 = uint32(hevcSequenceHeader[17]) & 0x07
	hevc.bitDepthChromaMinus8 = uint32(hevcSequenceHeader[18]) & 0x07
	hevc.avgFrameRate = (uint32(hevcSequenceHeader[19]) << 8) | uint32(hevcSequenceHeader[20])
	hevc.constantFrameRate = (uint32(hevcSequenceHeader[21]) >> 6) & 0x03
	hevc.numTemporalLayers = (uint32(hevcSequenceHeader[21]) >> 3) & 0x07
	hevc.temporalIdNested = (uint32(hevcSequenceHeader[21]) >> 2) & 0x01
	hevc.lengthSizeMinusOne = uint32(hevcSequenceHeader[21]) & 0x03

	numOfArrays := int(hevcSequenceHeader[22])
	p := hevcSequenceHeader[23:]
	for i := 0; i < numOfArrays; i++ {
		if len(p) < 3 {
			break
		}
		nalutype := p[0]
		n := (uint32(p[1]) << 8) | uint32(p[2])
		p = p[3:]
		for j := 0; j < int(n); j++ {
			if len(p) < 2 {
				break
			}
			k := (uint32(p[0]) << 8) | uint32(p[1])
			if len(p) < 2+int(k) {
				break
			}
			p = p[2:]
			if nalutype == 33 {
				// SPS
				sps := make([]byte, k)
				for x := 0; x < int(k); x++ {
					sps[x] = p[x]
				}
				hevc.psps = HEVCParseSPS(sps)
				info.profile = hevc.general_profile_idc
				info.level = float32(hevc.general_level_idc) / 30.0
				info.width = hevc.psps.pic_width_in_luma_samples - (hevc.psps.conf_win_left_offset + hevc.psps.conf_win_right_offset)
				info.height = hevc.psps.pic_height_in_luma_samples - (hevc.psps.conf_win_top_offset + hevc.psps.conf_win_bottom_offset)
			}
			p = p[k:]
		}
	}

	return info
}

/* Video config */

const AVC_CODEC_H264 = 7
const AVC_CODEC_HEVC = 12

type AVCSpecificConfig struct {
	codec uint32
	h264  H264SpecificConfig
	hevc  HEVCSpecificConfig
}

func readAVCSpecificConfig(avcSequenceHeader []byte) AVCSpecificConfig {
	codec_id := avcSequenceHeader[0] & 0x0f
	r := AVCSpecificConfig{
		codec: uint32(codec_id),
	}

	switch codec_id {
	case AVC_CODEC_H264:
		r.h264 = readH264SpecificConfig(avcSequenceHeader)
	case AVC_CODEC_HEVC:
		r.hevc = readHEVCSpecificConfig(avcSequenceHeader)
	}

	return r
}

func getAVCProfileName(info AVCSpecificConfig) string {
	switch info.codec {
	case AVC_CODEC_H264:
		switch info.h264.profile {
		case 1:
			return "Main"
		case 2:
			return "Main 10"
		case 3:
			return "Main Still Picture"
		case 66:
			return "Baseline"
		case 77:
			return "Main"
		case 100:
			return "High"
		default:
			return ""
		}
	case AVC_CODEC_HEVC:
		switch info.hevc.profile {
		case 1:
			return "Main"
		case 2:
			return "Main 10"
		case 3:
			return "Main Still Picture"
		case 66:
			return "Baseline"
		case 77:
			return "Main"
		case 100:
			return "High"
		default:
			return ""
		}
	default:
		return ""
	}
}
