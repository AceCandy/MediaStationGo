package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type rawProbe struct {
	Format   rawProbeFormat    `json:"format"`
	Streams  []rawProbeStream  `json:"streams"`
	Chapters []rawProbeChapter `json:"chapters"`
}

type rawProbeFormat struct {
	FormatName     string            `json:"format_name"`
	FormatLongName string            `json:"format_long_name"`
	StartTime      string            `json:"start_time"`
	Duration       string            `json:"duration"`
	Size           string            `json:"size"`
	BitRate        string            `json:"bit_rate"`
	ProbeScore     int               `json:"probe_score"`
	Tags           map[string]string `json:"tags"`
}

type rawProbeStream struct {
	Index              int                 `json:"index"`
	CodecType          string              `json:"codec_type"`
	CodecName          string              `json:"codec_name"`
	CodecLongName      string              `json:"codec_long_name"`
	Profile            string              `json:"profile"`
	Level              int                 `json:"level"`
	TimeBase           string              `json:"time_base"`
	StartTime          string              `json:"start_time"`
	Duration           string              `json:"duration"`
	BitRate            string              `json:"bit_rate"`
	Width              int                 `json:"width"`
	Height             int                 `json:"height"`
	SampleAspectRatio  string              `json:"sample_aspect_ratio"`
	DisplayAspectRatio string              `json:"display_aspect_ratio"`
	PixelFormat        string              `json:"pix_fmt"`
	BitsPerRawSample   string              `json:"bits_per_raw_sample"`
	BitsPerSample      int                 `json:"bits_per_sample"`
	ColorRange         string              `json:"color_range"`
	ColorSpace         string              `json:"color_space"`
	ColorTransfer      string              `json:"color_transfer"`
	ColorPrimaries     string              `json:"color_primaries"`
	AverageFrameRate   string              `json:"avg_frame_rate"`
	RealFrameRate      string              `json:"r_frame_rate"`
	SampleFormat       string              `json:"sample_fmt"`
	SampleRate         string              `json:"sample_rate"`
	Channels           int                 `json:"channels"`
	ChannelLayout      string              `json:"channel_layout"`
	Tags               map[string]string   `json:"tags"`
	Disposition        rawProbeDisposition `json:"disposition"`
	SideDataList       []rawProbeSideData  `json:"side_data_list"`
}

type rawProbeDisposition struct {
	Default         int `json:"default"`
	Dub             int `json:"dub"`
	Original        int `json:"original"`
	Comment         int `json:"comment"`
	Lyrics          int `json:"lyrics"`
	Karaoke         int `json:"karaoke"`
	Forced          int `json:"forced"`
	HearingImpaired int `json:"hearing_impaired"`
	VisualImpaired  int `json:"visual_impaired"`
	AttachedPic     int `json:"attached_pic"`
}

type rawProbeSideData struct {
	SideDataType string `json:"side_data_type"`
	MaxContent   int    `json:"max_content"`
	MaxAverage   int    `json:"max_average"`
	RedX         string `json:"red_x"`
	RedY         string `json:"red_y"`
	GreenX       string `json:"green_x"`
	GreenY       string `json:"green_y"`
	BlueX        string `json:"blue_x"`
	BlueY        string `json:"blue_y"`
	WhitePointX  string `json:"white_point_x"`
	WhitePointY  string `json:"white_point_y"`
	MinLuminance string `json:"min_luminance"`
	MaxLuminance string `json:"max_luminance"`
}

type rawProbeChapter struct {
	ID        int               `json:"id"`
	TimeBase  string            `json:"time_base"`
	StartTime string            `json:"start_time"`
	EndTime   string            `json:"end_time"`
	Tags      map[string]string `json:"tags"`
}

func parseProbeJSON(data []byte) (*ProbeResult, error) {
	var raw rawProbe
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse ffprobe json: %w", err)
	}
	doc := &ProbeDocument{
		SchemaVersion: ProbeDocumentSchemaVersion,
		Format: ProbeFormat{
			Name: raw.Format.FormatName, LongName: raw.Format.FormatLongName,
			StartTime: floatValue(raw.Format.StartTime), Duration: floatValue(raw.Format.Duration),
			Size: int64Value(raw.Format.Size), BitRate: int64Value(raw.Format.BitRate),
			ProbeScore: raw.Format.ProbeScore, Tags: safeProbeTags(raw.Format.Tags),
		},
		Streams:  make([]ProbeStream, 0, len(raw.Streams)),
		Chapters: make([]ProbeChapter, 0, len(raw.Chapters)),
	}
	res := &ProbeResult{Container: doc.Format.Name, DurationSec: int(doc.Format.Duration), Document: doc}
	for _, stream := range raw.Streams {
		s := normalizeProbeStream(stream)
		if s.Disposition.AttachedPic || (s.CodecType != "video" && s.CodecType != "audio" && s.CodecType != "subtitle") {
			continue
		}
		doc.Streams = append(doc.Streams, s)
		switch s.CodecType {
		case "video":
			if res.VideoCodec == "" && !s.Disposition.AttachedPic {
				res.VideoCodec, res.Width, res.Height = s.CodecName, s.Width, s.Height
			}
		case "audio":
			if res.AudioCodec == "" {
				res.AudioCodec = s.CodecName
			}
		}
	}
	for _, chapter := range raw.Chapters {
		doc.Chapters = append(doc.Chapters, ProbeChapter{
			ID: chapter.ID, TimeBase: chapter.TimeBase,
			StartTime: floatValue(chapter.StartTime), EndTime: floatValue(chapter.EndTime),
			Tags: safeProbeTags(chapter.Tags),
		})
	}
	return res, nil
}

func normalizeProbeStream(raw rawProbeStream) ProbeStream {
	sideData := make([]ProbeSideData, 0, len(raw.SideDataList))
	for _, side := range raw.SideDataList {
		sideData = append(sideData, ProbeSideData{
			Type: side.SideDataType, MaxContent: side.MaxContent, MaxAverage: side.MaxAverage,
			RedX: side.RedX, RedY: side.RedY, GreenX: side.GreenX, GreenY: side.GreenY,
			BlueX: side.BlueX, BlueY: side.BlueY, WhitePointX: side.WhitePointX,
			WhitePointY: side.WhitePointY, MinLuminance: side.MinLuminance, MaxLuminance: side.MaxLuminance,
		})
	}
	bitDepth := int(int64Value(raw.BitsPerRawSample))
	if bitDepth == 0 {
		bitDepth = raw.BitsPerSample
	}
	if bitDepth == 0 {
		bitDepth = pixelFormatBitDepth(raw.PixelFormat)
	}
	return ProbeStream{
		Index: raw.Index, CodecType: raw.CodecType, CodecName: raw.CodecName,
		CodecLongName: raw.CodecLongName, Profile: raw.Profile, Level: raw.Level,
		TimeBase: raw.TimeBase, StartTime: floatValue(raw.StartTime), Duration: floatValue(raw.Duration),
		BitRate: int64Value(raw.BitRate), Width: raw.Width, Height: raw.Height,
		SampleAspectRatio: raw.SampleAspectRatio, DisplayAspectRatio: raw.DisplayAspectRatio,
		PixelFormat: raw.PixelFormat, BitDepth: bitDepth, ColorRange: raw.ColorRange,
		ColorSpace: raw.ColorSpace, ColorTransfer: raw.ColorTransfer, ColorPrimaries: raw.ColorPrimaries,
		AverageFrameRate: raw.AverageFrameRate, RealFrameRate: raw.RealFrameRate,
		SampleFormat: raw.SampleFormat, SampleRate: int(int64Value(raw.SampleRate)), Channels: raw.Channels,
		ChannelLayout: raw.ChannelLayout, BitsPerSample: raw.BitsPerSample,
		Tags: safeProbeTags(raw.Tags), SideData: sideData,
		Disposition: ProbeDisposition{
			Default: raw.Disposition.Default != 0, Dub: raw.Disposition.Dub != 0,
			Original: raw.Disposition.Original != 0, Comment: raw.Disposition.Comment != 0,
			Lyrics: raw.Disposition.Lyrics != 0, Karaoke: raw.Disposition.Karaoke != 0,
			Forced: raw.Disposition.Forced != 0, HearingImpaired: raw.Disposition.HearingImpaired != 0,
			VisualImpaired: raw.Disposition.VisualImpaired != 0, AttachedPic: raw.Disposition.AttachedPic != 0,
		},
	}
}

func pixelFormatBitDepth(pixelFormat string) int {
	format := strings.ToLower(strings.TrimSpace(pixelFormat))
	if format == "" {
		return 0
	}
	match := pixelFormatBitDepthRE.FindStringSubmatch(format)
	if len(match) != 2 {
		return 0
	}
	depth, _ := strconv.Atoi(match[1])
	return depth
}

func safeProbeTags(tags map[string]string) ProbeTags {
	return ProbeTags{
		Language: tags["language"], Title: tags["title"], HandlerName: tags["handler_name"],
		Encoder: tags["encoder"], CreationTime: tags["creation_time"],
	}
}

func floatValue(value string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return v
}

func int64Value(value string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return v
}

var (
	pixelFormatBitDepthRE = regexp.MustCompile(`(?:p0?|p[024]|gray|xyz|y[24]|x2(?:rgb|bgr))(9|10|12|14|16)(?:le|be)$`)
	ffmpegDurationRE      = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
	ffmpegInputRE         = regexp.MustCompile(`Input #\d+,\s*(.+?),\s*from`)
	ffmpegVideoRE         = regexp.MustCompile(`Video:\s*([^,\s]+).*?(\d{2,5})x(\d{2,5})`)
	ffmpegAudioRE         = regexp.MustCompile(`Audio:\s*([^,\s]+)`)
)

func parseFFmpegProbeText(text string) *ProbeResult {
	res := &ProbeResult{}
	if match := ffmpegInputRE.FindStringSubmatch(text); len(match) == 2 {
		res.Container = strings.TrimSpace(match[1])
	}
	if match := ffmpegDurationRE.FindStringSubmatch(text); len(match) == 4 {
		hours, _ := strconv.Atoi(match[1])
		minutes, _ := strconv.Atoi(match[2])
		seconds, _ := strconv.ParseFloat(match[3], 64)
		res.DurationSec = hours*3600 + minutes*60 + int(seconds)
	}
	for _, line := range strings.Split(text, "\n") {
		if res.VideoCodec == "" {
			if match := ffmpegVideoRE.FindStringSubmatch(line); len(match) == 4 {
				res.VideoCodec = strings.TrimSpace(match[1])
				res.Width, _ = strconv.Atoi(match[2])
				res.Height, _ = strconv.Atoi(match[3])
			}
		}
		if res.AudioCodec == "" {
			if match := ffmpegAudioRE.FindStringSubmatch(line); len(match) == 2 {
				res.AudioCodec = strings.TrimSpace(match[1])
			}
		}
	}
	return res
}
