package textureimport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const MaxBytes = 8 << 20

var (
	ErrSize       = errors.New("PNG 文件不能超过 8 MiB")
	ErrPNG        = errors.New("PNG 图像数据无效")
	ErrDimensions = errors.New("PNG 尺寸不符合 Minecraft 皮肤或披风比例，最长边为 1024 像素")
	tagPattern    = regexp.MustCompile(`^[\pL\pN][\pL\pN _-]{0,23}$`)
)

func ReadPNG(file io.Reader, kind, name string) ([]byte, string, error) {
	return ReadPNGWithPolicy(file, kind, name, DefaultContentPolicy())
}

func ReadPNGWithPolicy(file io.Reader, kind, name string, policy ContentPolicy) ([]byte, string, error) {
	data, config, err := DecodePNG(file)
	if err != nil {
		return nil, kind, err
	}
	kind, err = ValidatePNGForPolicy(kind, name, config.Width, config.Height, len(data), policy)
	if err != nil {
		return nil, kind, err
	}
	return data, kind, nil
}

func DecodePNG(file io.Reader) ([]byte, image.Config, error) {
	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil || len(data) > MaxBytes {
		return nil, image.Config{}, ErrSize
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, image.Config{}, ErrPNG
	}
	if config.Width > 1024 || config.Height > 1024 {
		return nil, config, ErrDimensions
	}
	if _, err = png.Decode(bytes.NewReader(data)); err != nil {
		return nil, config, ErrPNG
	}
	return data, config, nil
}

func ValidatePNGForPolicy(kind, name string, width, height, size int, policy ContentPolicy) (string, error) {
	policy = policy.Normalized()
	if kind == "auto" {
		kind = "skin"
		base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		if ValidDimensionsForPolicy("cape", width, height, policy) && (!ValidDimensionsForPolicy("skin", width, height, policy) || strings.Contains(base, "cape") || strings.Contains(base, "披风")) {
			kind = "cape"
		}
	}
	if int64(size) > policy.MaxFileBytes {
		if policy.MaxFileBytes == MaxBytes {
			return kind, ErrSize
		}
		return kind, fmt.Errorf("PNG 文件不能超过 %g MiB", float64(policy.MaxFileBytes)/(1<<20))
	}
	if !ValidDimensionsForPolicy(kind, width, height, policy) {
		switch kind {
		case "skin":
			if policy.MaxSkinWidth == 128 {
				return kind, errors.New("皮肤尺寸仅支持 64×64、64×32、128×128 或 128×64 像素")
			}
			return kind, fmt.Errorf("皮肤宽度须为 64 到 %d 像素之间的 2 的幂，高度须等于宽度或宽度的一半", policy.MaxSkinWidth)
		case "cape":
			return kind, fmt.Errorf("披风宽度须为 64 到 %d 像素之间的 64 的倍数，高度须为宽度的一半", policy.MaxCapeWidth)
		default:
			return kind, ErrDimensions
		}
	}
	return kind, nil
}

func ValidDimensions(kind string, width, height int) bool {
	return ValidDimensionsForPolicy(kind, width, height, DefaultContentPolicy())
}

func ValidDimensionsForPolicy(kind string, width, height int, policy ContentPolicy) bool {
	policy = policy.Normalized()
	if width < 64 {
		return false
	}
	if kind == "cape" {
		return width <= policy.MaxCapeWidth && width%64 == 0 && height == width/2
	}
	return kind == "skin" && width <= policy.MaxSkinWidth && width&(width-1) == 0 && (height == width || height == width/2)
}

func NormalizeTags(raw []string) ([]string, error) {
	if len(raw) > 10 {
		return nil, errors.New("too many tags")
	}
	tags := []string{}
	for _, tag := range raw {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if !tagPattern.MatchString(tag) {
			return nil, errors.New("invalid tag")
		}
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

type RowReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type ContentPolicy struct {
	ReviewUploads bool     `json:"reviewUploads"`
	Categories    []string `json:"categories"`
	MaxSkinWidth  int      `json:"maxSkinWidth"`
	MaxCapeWidth  int      `json:"maxCapeWidth"`
	MaxFileBytes  int64    `json:"maxFileBytes"`
}

func DefaultContentPolicy() ContentPolicy {
	return ContentPolicy{Categories: []string{}, MaxSkinWidth: 128, MaxCapeWidth: 1024, MaxFileBytes: MaxBytes}
}

func (policy ContentPolicy) Normalized() ContentPolicy {
	defaults := DefaultContentPolicy()
	if policy.Categories == nil {
		policy.Categories = defaults.Categories
	}
	if policy.MaxSkinWidth == 0 {
		policy.MaxSkinWidth = defaults.MaxSkinWidth
	}
	if policy.MaxCapeWidth == 0 {
		policy.MaxCapeWidth = defaults.MaxCapeWidth
	}
	if policy.MaxFileBytes == 0 {
		policy.MaxFileBytes = defaults.MaxFileBytes
	}
	return policy
}

func (policy ContentPolicy) ValidLimits() bool {
	widths := []int{64, 128, 256, 512, 1024}
	return slices.Contains(widths, policy.MaxSkinWidth) && slices.Contains(widths, policy.MaxCapeWidth) && policy.MaxFileBytes >= 1<<20 && policy.MaxFileBytes <= MaxBytes
}

func (policy ContentPolicy) ValidCategory(category string) bool {
	return category == "" || slices.Contains(policy.Categories, category)
}

func ReadContentPolicy(ctx context.Context, reader RowReader) (ContentPolicy, error) {
	var policy ContentPolicy
	err := readSetting(ctx, reader, "SELECT value FROM settings WHERE setting_key='content_policy'", &policy)
	return policy.Normalized(), err
}

func DecodeJSONString(raw string, out any) error {
	return json.Unmarshal([]byte(raw), out)
}

func readSetting(ctx context.Context, reader RowReader, query string, out any) error {
	var raw string
	err := reader.QueryRowContext(ctx, query).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), out)
}
