package service

import (
	"encoding/json"
	"errors"
	"fmt"
)

const ProbeDocumentSchemaVersion = 1

// ProbeDocument 是允许持久化的 ffprobe 字段白名单，不包含输入路径、URL 或请求凭据。
type ProbeDocument struct {
	SchemaVersion int            `json:"schema_version"`
	Format        ProbeFormat    `json:"format"`
	Streams       []ProbeStream  `json:"streams"`
	Chapters      []ProbeChapter `json:"chapters,omitempty"`
}

type ProbeFormat struct {
	Name       string    `json:"name,omitempty"`
	LongName   string    `json:"long_name,omitempty"`
	StartTime  float64   `json:"start_time,omitempty"`
	Duration   float64   `json:"duration,omitempty"`
	Size       int64     `json:"size,omitempty"`
	BitRate    int64     `json:"bit_rate,omitempty"`
	ProbeScore int       `json:"probe_score,omitempty"`
	Tags       ProbeTags `json:"tags,omitempty"`
}

// ProbeStream.Index 保留 ffprobe 的绝对 stream index，后续选轨不得改用数组位置。
type ProbeStream struct {
	Index              int              `json:"index"`
	CodecType          string           `json:"codec_type"`
	CodecName          string           `json:"codec_name,omitempty"`
	CodecLongName      string           `json:"codec_long_name,omitempty"`
	Profile            string           `json:"profile,omitempty"`
	Level              int              `json:"level,omitempty"`
	TimeBase           string           `json:"time_base,omitempty"`
	StartTime          float64          `json:"start_time,omitempty"`
	Duration           float64          `json:"duration,omitempty"`
	BitRate            int64            `json:"bit_rate,omitempty"`
	Width              int              `json:"width,omitempty"`
	Height             int              `json:"height,omitempty"`
	SampleAspectRatio  string           `json:"sample_aspect_ratio,omitempty"`
	DisplayAspectRatio string           `json:"display_aspect_ratio,omitempty"`
	PixelFormat        string           `json:"pixel_format,omitempty"`
	BitDepth           int              `json:"bit_depth,omitempty"`
	ColorRange         string           `json:"color_range,omitempty"`
	ColorSpace         string           `json:"color_space,omitempty"`
	ColorTransfer      string           `json:"color_transfer,omitempty"`
	ColorPrimaries     string           `json:"color_primaries,omitempty"`
	AverageFrameRate   string           `json:"average_frame_rate,omitempty"`
	RealFrameRate      string           `json:"real_frame_rate,omitempty"`
	SampleFormat       string           `json:"sample_format,omitempty"`
	SampleRate         int              `json:"sample_rate,omitempty"`
	Channels           int              `json:"channels,omitempty"`
	ChannelLayout      string           `json:"channel_layout,omitempty"`
	BitsPerSample      int              `json:"bits_per_sample,omitempty"`
	Tags               ProbeTags        `json:"tags,omitempty"`
	Disposition        ProbeDisposition `json:"disposition,omitempty"`
	SideData           []ProbeSideData  `json:"side_data,omitempty"`
}

type ProbeDisposition struct {
	Default         bool `json:"default,omitempty"`
	Dub             bool `json:"dub,omitempty"`
	Original        bool `json:"original,omitempty"`
	Comment         bool `json:"comment,omitempty"`
	Lyrics          bool `json:"lyrics,omitempty"`
	Karaoke         bool `json:"karaoke,omitempty"`
	Forced          bool `json:"forced,omitempty"`
	HearingImpaired bool `json:"hearing_impaired,omitempty"`
	VisualImpaired  bool `json:"visual_impaired,omitempty"`
	AttachedPic     bool `json:"attached_pic,omitempty"`
}

type ProbeTags struct {
	Language     string `json:"language,omitempty"`
	Title        string `json:"title,omitempty"`
	HandlerName  string `json:"handler_name,omitempty"`
	Encoder      string `json:"encoder,omitempty"`
	CreationTime string `json:"creation_time,omitempty"`
}

type ProbeSideData struct {
	Type         string `json:"type,omitempty"`
	MaxContent   int    `json:"max_content,omitempty"`
	MaxAverage   int    `json:"max_average,omitempty"`
	RedX         string `json:"red_x,omitempty"`
	RedY         string `json:"red_y,omitempty"`
	GreenX       string `json:"green_x,omitempty"`
	GreenY       string `json:"green_y,omitempty"`
	BlueX        string `json:"blue_x,omitempty"`
	BlueY        string `json:"blue_y,omitempty"`
	WhitePointX  string `json:"white_point_x,omitempty"`
	WhitePointY  string `json:"white_point_y,omitempty"`
	MinLuminance string `json:"min_luminance,omitempty"`
	MaxLuminance string `json:"max_luminance,omitempty"`
}

type ProbeChapter struct {
	ID        int       `json:"id"`
	TimeBase  string    `json:"time_base,omitempty"`
	StartTime float64   `json:"start_time,omitempty"`
	EndTime   float64   `json:"end_time,omitempty"`
	Tags      ProbeTags `json:"tags,omitempty"`
}

func MarshalProbeDocument(doc *ProbeDocument) (string, error) {
	if err := validateProbeDocument(doc); err != nil {
		return "", err
	}
	b, err := json.Marshal(doc)
	return string(b), err
}

func UnmarshalProbeDocument(data string, schemaVersion int) (*ProbeDocument, error) {
	if schemaVersion != ProbeDocumentSchemaVersion {
		return nil, errors.New("unsupported probe document version")
	}
	var doc ProbeDocument
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		return nil, err
	}
	if doc.SchemaVersion != schemaVersion {
		return nil, errors.New("probe document version mismatch")
	}
	if err := validateProbeDocument(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func validateProbeDocument(doc *ProbeDocument) error {
	if doc == nil || doc.SchemaVersion != ProbeDocumentSchemaVersion {
		return errors.New("invalid probe document version")
	}
	seen := make(map[int]struct{}, len(doc.Streams))
	for _, stream := range doc.Streams {
		if stream.Index < 0 {
			return fmt.Errorf("invalid probe stream index %d", stream.Index)
		}
		if _, ok := seen[stream.Index]; ok {
			return fmt.Errorf("duplicate probe stream index %d", stream.Index)
		}
		seen[stream.Index] = struct{}{}
		if stream.Disposition.AttachedPic {
			return fmt.Errorf("attached picture stream %d is not persistable", stream.Index)
		}
		switch stream.CodecType {
		case "video", "audio", "subtitle":
		default:
			return fmt.Errorf("unsupported probe stream type %q", stream.CodecType)
		}
	}
	return nil
}
