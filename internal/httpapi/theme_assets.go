package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wishingskin/internal/store"
)

const (
	maxThemeArchiveBytes    = 80 << 20
	maxThemeZipEntryBytes   = 2 << 20
	maxThemeTextureDim      = 64
	maxThemeAssetsJSONBytes = 4 << 20
	maxThemePNGPixels       = 4 << 20
	maxThemePackPixels      = 32 << 20
	themeArchiveTimeout     = 3 * time.Minute
)

type storedThemeAssets struct {
	Version    string            `json:"version"`
	SourceName string            `json:"sourceName"`
	ImportedAt string            `json:"importedAt"`
	Count      int               `json:"count"`
	Textures   map[string]string `json:"textures"`
}

type publicThemeAssets = store.ThemeAssetsMetadata

type tintSpec struct {
	kind     string // "", "grass", "foliage", "fixed"
	temp     float64
	downfall float64
	fallback color.NRGBA
}

type themeTextureSpec struct {
	key        string
	namespace  string // "" / "block" or "particle"
	candidates []string
	overlay    string
	tint       tintSpec
}

var (
	plainsGrassTint = tintSpec{kind: "grass", temp: 0.80, downfall: 0.40, fallback: color.NRGBA{145, 189, 89, 255}}
	plainsLeafTint  = tintSpec{kind: "foliage", temp: 0.80, downfall: 0.40, fallback: color.NRGBA{119, 171, 47, 255}}
	jungleGrassTint = tintSpec{kind: "grass", temp: 0.95, downfall: 0.90, fallback: color.NRGBA{89, 201, 60, 255}}
	jungleLeafTint  = tintSpec{kind: "foliage", temp: 0.95, downfall: 0.90, fallback: color.NRGBA{48, 187, 11, 255}}
	cherryGrassTint = tintSpec{kind: "grass", temp: 0.50, downfall: 0.80, fallback: color.NRGBA{182, 219, 97, 255}}
)

var themeTextureSpecs = []themeTextureSpec{
	// Core & Plains / Mineshaft
	{key: "plains_grass_top", candidates: []string{"grass_block_top"}, tint: plainsGrassTint},
	{key: "plains_grass_side", candidates: []string{"grass_block_side"}, overlay: "grass_block_side_overlay", tint: plainsGrassTint},
	{key: "plains_oak_leaves", candidates: []string{"oak_leaves"}, tint: plainsLeafTint},
	{key: "dirt", candidates: []string{"dirt"}},
	{key: "stone", candidates: []string{"stone"}},
	{key: "cobblestone", candidates: []string{"cobblestone"}},
	{key: "oak_planks", candidates: []string{"oak_planks"}},
	{key: "oak_log", candidates: []string{"oak_log"}},
	{key: "deepslate", candidates: []string{"deepslate"}},
	{key: "cobbled_deepslate", candidates: []string{"cobbled_deepslate", "deepslate"}},
	{key: "deepslate_diamond_ore", candidates: []string{"deepslate_diamond_ore", "diamond_ore"}},
	{key: "deepslate_iron_ore", candidates: []string{"deepslate_iron_ore", "iron_ore"}},
	{key: "deepslate_gold_ore", candidates: []string{"deepslate_gold_ore", "gold_ore"}},
	{key: "coal_ore", candidates: []string{"deepslate_coal_ore", "coal_ore"}},
	{key: "rail", candidates: []string{"rail"}},
	{key: "bedrock", candidates: []string{"bedrock"}},

	// Snowy & 26.4 Ice Caves
	{key: "snow", candidates: []string{"snow"}},
	{key: "grass_block_side_snowed", candidates: []string{"grass_block_side_snowed", "snow"}},
	{key: "ice", candidates: []string{"ice"}},
	{key: "packed_ice", candidates: []string{"packed_ice"}},
	{key: "blue_ice", candidates: []string{"blue_ice", "packed_ice"}},
	{key: "spruce_planks", candidates: []string{"spruce_planks"}},
	{key: "spruce_log", candidates: []string{"spruce_log"}},
	{key: "ice_crystal", candidates: []string{"ice_crystal", "blue_ice", "packed_ice"}},
	{key: "icicle_tip", candidates: []string{"icicle_tip", "icicle_middle", "ice"}},
	{key: "packed_ice_bricks", candidates: []string{"packed_ice_bricks", "packed_ice"}},
	{key: "chiseled_packed_ice", candidates: []string{"chiseled_packed_ice", "blue_ice"}},
	{key: "calcite", candidates: []string{"calcite", "diorite"}},
	{key: "polished_calcite_bricks", candidates: []string{"polished_calcite_bricks", "polished_calcite", "calcite"}},

	// Jungle & Lush Caves
	{key: "jungle_grass_top", candidates: []string{"grass_block_top"}, tint: jungleGrassTint},
	{key: "jungle_grass_side", candidates: []string{"grass_block_side"}, overlay: "grass_block_side_overlay", tint: jungleGrassTint},
	{key: "jungle_leaves", candidates: []string{"jungle_leaves"}, tint: jungleLeafTint},
	{key: "jungle_log", candidates: []string{"jungle_log"}},
	{key: "jungle_planks", candidates: []string{"jungle_planks"}},
	{key: "bamboo_stalk", candidates: []string{"bamboo_stalk", "jungle_log"}},
	{key: "mossy_cobblestone", candidates: []string{"mossy_cobblestone"}},
	{key: "moss_block", candidates: []string{"moss_block", "mossy_cobblestone"}},
	{key: "rooted_dirt", candidates: []string{"rooted_dirt", "dirt"}},
	{key: "clay", candidates: []string{"clay"}},
	{key: "glow_lichen", candidates: []string{"glow_lichen"}},
	{key: "spore_blossom", candidates: []string{"spore_blossom"}},
	{key: "big_dripleaf_top", candidates: []string{"big_dripleaf_top", "moss_block"}},

	// Desert & Desert Temple Archaeology
	{key: "sand", candidates: []string{"sand"}},
	{key: "sandstone", candidates: []string{"sandstone"}},
	{key: "sandstone_top", candidates: []string{"sandstone_top", "sandstone"}},
	{key: "cut_sandstone", candidates: []string{"cut_sandstone", "sandstone"}},
	{key: "chiseled_sandstone", candidates: []string{"chiseled_sandstone", "sandstone"}},
	{key: "red_sand", candidates: []string{"red_sand", "sand"}},
	{key: "orange_terracotta", candidates: []string{"orange_terracotta"}},
	{key: "blue_terracotta", candidates: []string{"blue_terracotta"}},
	{key: "yellow_terracotta", candidates: []string{"yellow_terracotta"}},
	{key: "suspicious_sand", candidates: []string{"suspicious_sand_0", "suspicious_sand_1", "suspicious_sand", "sand"}},
	{key: "tnt_side", candidates: []string{"tnt_side"}},

	// Cherry Grove & Amethyst Geode
	{key: "cherry_grass_top", candidates: []string{"grass_block_top"}, tint: cherryGrassTint},
	{key: "cherry_grass_side", candidates: []string{"grass_block_side"}, overlay: "grass_block_side_overlay", tint: cherryGrassTint},
	{key: "cherry_leaves", candidates: []string{"cherry_leaves", "pink_wool"}},
	{key: "cherry_log", candidates: []string{"cherry_log", "birch_log"}},
	{key: "cherry_planks", candidates: []string{"cherry_planks", "birch_planks"}},
	{key: "pink_petals", candidates: []string{"pink_petals"}},
	{key: "amethyst_block", candidates: []string{"amethyst_block", "purple_concrete"}},
	{key: "budding_amethyst", candidates: []string{"budding_amethyst", "amethyst_block"}},
	{key: "amethyst_cluster", candidates: []string{"amethyst_cluster", "large_amethyst_bud"}},

	// Nether (Crimson <-> Warped)
	{key: "crimson_nylium", candidates: []string{"crimson_nylium", "netherrack"}},
	{key: "crimson_nylium_side", candidates: []string{"crimson_nylium_side", "crimson_nylium", "netherrack"}},
	{key: "crimson_planks", candidates: []string{"crimson_planks", "nether_bricks"}},
	{key: "crimson_stem", candidates: []string{"crimson_stem", "crimson_planks"}},
	{key: "nether_wart_block", candidates: []string{"nether_wart_block"}},
	{key: "netherrack", candidates: []string{"netherrack"}},
	{key: "nether_bricks", candidates: []string{"nether_bricks", "nether_brick"}},
	{key: "shroomlight", candidates: []string{"shroomlight", "glowstone"}},
	{key: "magma", candidates: []string{"magma"}},
	{key: "warped_nylium", candidates: []string{"warped_nylium", "netherrack"}},
	{key: "warped_nylium_side", candidates: []string{"warped_nylium_side", "warped_nylium", "netherrack"}},
	{key: "warped_planks", candidates: []string{"warped_planks", "warped_nylium"}},
	{key: "warped_stem", candidates: []string{"warped_stem", "warped_planks"}},
	{key: "warped_wart_block", candidates: []string{"warped_wart_block", "warped_nylium"}},
	{key: "basalt_side", candidates: []string{"basalt_side", "basalt_top"}},
	{key: "blackstone", candidates: []string{"blackstone", "obsidian"}},

	// End (End City <-> Outer Void)
	{key: "end_stone", candidates: []string{"end_stone"}},
	{key: "end_stone_bricks", candidates: []string{"end_stone_bricks", "end_stone"}},
	{key: "purpur_block", candidates: []string{"purpur_block"}},
	{key: "purpur_pillar", candidates: []string{"purpur_pillar_side", "purpur_pillar", "purpur_block"}},
	{key: "obsidian", candidates: []string{"obsidian"}},
	{key: "crying_obsidian", candidates: []string{"crying_obsidian", "obsidian"}},
	{key: "gilded_blackstone", candidates: []string{"gilded_blackstone", "blackstone"}},

	// Deep Dark (Ancient City)
	{key: "sculk", candidates: []string{"sculk", "obsidian"}},
	{key: "sculk_vein", candidates: []string{"sculk_vein", "sculk"}},
	{key: "sculk_catalyst_top", candidates: []string{"sculk_catalyst_top", "sculk"}},
	{key: "sculk_catalyst_side", candidates: []string{"sculk_catalyst_side", "sculk"}},
	{key: "sculk_sensor_top", candidates: []string{"sculk_sensor_top", "sculk"}},
	{key: "sculk_shrieker_top", candidates: []string{"sculk_shrieker_top", "sculk"}},
	{key: "reinforced_deepslate_top", candidates: []string{"reinforced_deepslate_top", "deepslate"}},
	{key: "reinforced_deepslate_side", candidates: []string{"reinforced_deepslate_side", "deepslate"}},
	{key: "tuff", candidates: []string{"tuff", "deepslate"}},

	// Authentic Minecraft Particle Textures (assets/minecraft/textures/particle/)
	{key: "particle_cherry_0", namespace: "particle", candidates: []string{"cherry_0", "cherry_1"}},
	{key: "particle_cherry_1", namespace: "particle", candidates: []string{"cherry_2", "cherry_3"}},
	{key: "particle_cherry_2", namespace: "particle", candidates: []string{"cherry_5", "cherry_4"}},
	{key: "particle_cherry_3", namespace: "particle", candidates: []string{"cherry_8", "cherry_7"}},
	{key: "particle_leaf_plains_0", namespace: "particle", candidates: []string{"leaf_0", "leaf_1"}, tint: plainsLeafTint},
	{key: "particle_leaf_plains_1", namespace: "particle", candidates: []string{"leaf_3", "leaf_2"}, tint: plainsLeafTint},
	{key: "particle_leaf_jungle_0", namespace: "particle", candidates: []string{"leaf_0", "leaf_1"}, tint: jungleLeafTint},
	{key: "particle_leaf_jungle_1", namespace: "particle", candidates: []string{"leaf_4", "leaf_2"}, tint: jungleLeafTint},
	{key: "particle_firefly", namespace: "particle", candidates: []string{"firefly", "glow"}},
	{key: "particle_sculk_soul_0", namespace: "particle", candidates: []string{"sculk_soul_0", "soul_0"}},
	{key: "particle_sculk_soul_1", namespace: "particle", candidates: []string{"sculk_soul_3", "soul_3"}},
	{key: "particle_sculk_soul_2", namespace: "particle", candidates: []string{"sculk_soul_6", "soul_6"}},
	{key: "particle_flame", namespace: "particle", candidates: []string{"flame"}},
	{key: "particle_soul_flame", namespace: "particle", candidates: []string{"soul_fire_flame", "copper_fire_flame", "flame"}},
	{key: "particle_glow", namespace: "particle", candidates: []string{"glow", "glint"}},
	{key: "particle_glint", namespace: "particle", candidates: []string{"glint", "glow"}},
	{key: "particle_spark_0", namespace: "particle", candidates: []string{"spark_0", "spark_1", "glint"}},
	{key: "particle_spark_1", namespace: "particle", candidates: []string{"spark_3", "spark_2", "glow"}},
}

var allowedThemeTextureKeys = func() map[string]bool {
	m := make(map[string]bool, len(themeTextureSpecs))
	for _, spec := range themeTextureSpecs {
		m[spec.key] = true
	}
	return m
}()

func (a *Server) themeAssets(ctx context.Context) (publicThemeAssets, error) {
	value := publicThemeAssets{Keys: []string{}}
	_, err := a.readSetting(ctx, store.ThemeAssetsSetting, &value)
	if value.Version == "" || len(value.Keys) == 0 {
		return publicThemeAssets{Keys: []string{}}, err
	}
	return value, err
}

func summarizeThemeAssets(stored storedThemeAssets) publicThemeAssets {
	if stored.Version == "" || len(stored.Textures) == 0 {
		return publicThemeAssets{Keys: []string{}}
	}
	keys := make([]string, 0, len(stored.Textures))
	for key := range stored.Textures {
		if allowedThemeTextureKeys[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return publicThemeAssets{
		Version:    stored.Version,
		SourceName: stored.SourceName,
		ImportedAt: stored.ImportedAt,
		Count:      len(keys),
		Keys:       keys,
	}
}

func (a *Server) adminThemeAssets(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	metadata, err := a.themeAssets(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取主题材质资源")
		return
	}
	respond(w, 200, metadata)
}

func (a *Server) uploadThemeAssets(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	select {
	case a.uploads <- struct{}{}:
		defer func() { <-a.uploads }()
	default:
		failure(w, 429, "资源处理繁忙，请稍后重试")
		return
	}
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(themeArchiveTimeout)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline.Add(30 * time.Second))
	r.Body = http.MaxBytesReader(w, r.Body, maxThemeArchiveBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		failure(w, 400, "请上传 Minecraft 客户端 .jar 或材质包 .zip 文件")
		return
	}
	var tmpPath, sourceName string
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			failure(w, 400, "上传文件超过 80 MiB 限制或内容无效")
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		sourceName = sanitizeAssetFilename(part.FileName())
		tmp, err := os.CreateTemp("", "wishingskin-pack-*.zip")
		if err != nil {
			_ = part.Close()
			failure(w, 503, "暂时无法处理上传文件")
			return
		}
		tmpPath = tmp.Name()
		written, copyErr := io.Copy(tmp, io.LimitReader(part, maxThemeArchiveBytes+1))
		closeErr := tmp.Close()
		_ = part.Close()
		if copyErr != nil || closeErr != nil {
			failure(w, 400, "读取上传文件失败")
			return
		}
		if written > maxThemeArchiveBytes {
			failure(w, 400, "文件不能超过 80 MiB")
			return
		}
		break
	}
	if tmpPath == "" {
		failure(w, 400, "请选择要导入的 .jar 或 .zip 文件")
		return
	}
	stored, err := extractThemeTexturesFromZip(tmpPath, sourceName)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	encoded, err := json.Marshal(stored)
	if err != nil || len(encoded) > maxThemeAssetsJSONBytes {
		failure(w, 400, "提取的贴图数据过大，请使用标准分辨率资源包")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	metadata := summarizeThemeAssets(stored)
	ctx := r.Context()
	_, err = tx.ExecContext(ctx, a.s.Bind("DELETE FROM settings WHERE setting_key LIKE ?"), store.ThemeTextureSettingPrefix+"%")
	if err == nil {
		for _, key := range metadata.Keys {
			err = a.s.PutThemeAsset(ctx, tx, store.ThemeTextureSettingPrefix+key, metadata.Version, stored.Textures[key])
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		err = a.putSetting(ctx, tx, store.ThemeAssetsSetting, metadata)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.theme_assets", "site", "theme_assets")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存主题材质资源")
		return
	}
	respond(w, 200, metadata)
}

func (a *Server) deleteThemeAssets(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	_, err := tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM settings WHERE setting_key LIKE ?"), store.ThemeTextureSettingPrefix+"%")
	if err == nil {
		err = a.putSetting(r.Context(), tx, store.ThemeAssetsSetting, publicThemeAssets{Keys: []string{}})
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.theme_assets", "site", "theme_assets")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法清除主题材质资源")
		return
	}
	respond(w, 200, publicThemeAssets{Keys: []string{}})
}

func (a *Server) serveThemeAsset(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	key := strings.TrimSuffix(r.PathValue("key"), ".png")
	if !allowedThemeTextureKeys[key] {
		http.NotFound(w, r)
		return
	}
	storedVersion, raw, err := a.s.ReadThemeAsset(r.Context(), store.ThemeTextureSettingPrefix+key)
	if err != nil {
		failure(w, 503, "暂时无法读取主题材质资源")
		return
	}
	if storedVersion == "" || raw == "" {
		http.NotFound(w, r)
		return
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	if version != "" && version == storedVersion {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=300")
	}
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func sanitizeAssetFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "minecraft-pack.zip"
	}
	runes := []rune(base)
	if len(runes) > 120 {
		base = string(runes[:120])
	}
	return base
}

func extractThemeTexturesFromZip(zipPath, sourceName string) (storedThemeAssets, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return storedThemeAssets{}, errors.New("无法解析压缩包，请确认上传的是有效的 Minecraft .jar 或资源包 .zip")
	}
	defer zr.Close()

	blockFiles := map[string]*zip.File{}
	particleFiles := map[string]*zip.File{}
	var grassCmapFile, foliageCmapFile *zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if idx := strings.Index(name, "assets/minecraft/textures/block/"); idx >= 0 {
			rel := name[idx+len("assets/minecraft/textures/block/"):]
			if strings.HasSuffix(rel, ".png") && !strings.Contains(rel, "/") {
				blockName := strings.TrimSuffix(rel, ".png")
				if _, exists := blockFiles[blockName]; !exists {
					blockFiles[blockName] = f
				}
			}
			continue
		}
		if idx := strings.Index(name, "assets/minecraft/textures/particle/"); idx >= 0 {
			rel := name[idx+len("assets/minecraft/textures/particle/"):]
			if strings.HasSuffix(rel, ".png") && !strings.Contains(rel, "/") {
				pName := strings.TrimSuffix(rel, ".png")
				if _, exists := particleFiles[pName]; !exists {
					particleFiles[pName] = f
				}
			}
			continue
		}
		if strings.HasSuffix(name, "assets/minecraft/textures/colormap/grass.png") && grassCmapFile == nil {
			grassCmapFile = f
		} else if strings.HasSuffix(name, "assets/minecraft/textures/colormap/foliage.png") && foliageCmapFile == nil {
			foliageCmapFile = f
		}
	}
	if len(blockFiles) == 0 {
		return storedThemeAssets{}, errors.New("压缩包内未找到 assets/minecraft/textures/block/ 方块材质")
	}

	pixelsRemaining := maxThemePackPixels
	var grassCmap, foliageCmap image.Image
	var limitErr error
	decode := func(f *zip.File) image.Image {
		if f == nil || limitErr != nil {
			return nil
		}
		img, err := decodeZipPNG(f, &pixelsRemaining)
		if errors.Is(err, errThemePNGLimit) {
			limitErr = err
		}
		return img
	}
	if grassCmapFile != nil {
		grassCmap = decode(grassCmapFile)
	}
	if foliageCmapFile != nil {
		foliageCmap = decode(foliageCmapFile)
	}

	rawCache := map[string]*image.NRGBA{}
	loadSprite := func(ns, name string) *image.NRGBA {
		cacheKey := ns + ":" + name
		if cached, ok := rawCache[cacheKey]; ok {
			return cached
		}
		files := blockFiles
		if ns == "particle" {
			files = particleFiles
		}
		f, ok := files[name]
		if !ok {
			return nil
		}
		img := decode(f)
		if img == nil {
			rawCache[cacheKey] = nil
			return nil
		}
		normalized := normalizeBlockFrame(img)
		rawCache[cacheKey] = normalized
		return normalized
	}

	textures := make(map[string]string, len(themeTextureSpecs))
	hasher := sha256.New()
	for _, spec := range themeTextureSpecs {
		ns := spec.namespace
		if ns == "" {
			ns = "block"
		}
		var base *image.NRGBA
		for _, candidate := range spec.candidates {
			if img := loadSprite(ns, candidate); img != nil {
				base = cloneNRGBA(img)
				break
			}
		}
		if base == nil {
			continue
		}
		if spec.overlay != "" {
			if ov := loadSprite(ns, spec.overlay); ov != nil {
				tintColor := resolveTint(spec.tint, grassCmap, foliageCmap)
				tintedOv := applyMultiplyTint(cloneNRGBA(ov), tintColor)
				compositeOver(base, tintedOv)
			}
		} else if spec.tint.kind != "" {
			tintColor := resolveTint(spec.tint, grassCmap, foliageCmap)
			base = applyMultiplyTint(base, tintColor)
		}
		var out bytes.Buffer
		if err := png.Encode(&out, base); err != nil {
			continue
		}
		encoded := base64.StdEncoding.EncodeToString(out.Bytes())
		textures[spec.key] = encoded
		hasher.Write([]byte(spec.key))
		hasher.Write(out.Bytes())
	}

	if limitErr != nil {
		return storedThemeAssets{}, limitErr
	}
	if len(textures) < 8 {
		return storedThemeAssets{}, errors.New("提取到的有效方块贴图过少，请确认文件是完整的 Minecraft 客户端或方块资源包")
	}

	return storedThemeAssets{
		Version:    hex.EncodeToString(hasher.Sum(nil))[:16],
		SourceName: sourceName,
		ImportedAt: time.Now().UTC().Format(time.RFC3339),
		Count:      len(textures),
		Textures:   textures,
	}, nil
}

var errThemePNGLimit = errors.New("提取的贴图数据过大，请使用标准分辨率资源包")

func decodeZipPNG(f *zip.File, pixelsRemaining *int) (image.Image, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxThemeZipEntryBytes+1))
	if err != nil || len(data) > maxThemeZipEntryBytes {
		return nil, errors.New("entry too large")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxThemePNGPixels/config.Height || config.Width*config.Height > *pixelsRemaining {
		return nil, errThemePNGLimit
	}
	*pixelsRemaining -= config.Width * config.Height
	return png.Decode(bytes.NewReader(data))
}

func normalizeBlockFrame(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	// Animated Minecraft textures stack square frames vertically.
	if h > w && w > 0 && h%w == 0 {
		h = w
	}
	targetW, targetH := w, h
	if targetW > maxThemeTextureDim || targetH > maxThemeTextureDim {
		if targetW >= targetH {
			targetH = max(1, targetH*maxThemeTextureDim/targetW)
			targetW = maxThemeTextureDim
		} else {
			targetW = max(1, targetW*maxThemeTextureDim/targetH)
			targetH = maxThemeTextureDim
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
	for y := 0; y < targetH; y++ {
		sy := b.Min.Y + y*h/targetH
		for x := 0; x < targetW; x++ {
			sx := b.Min.X + x*w/targetW
			dst.Set(x, y, color.NRGBAModel.Convert(src.At(sx, sy)))
		}
	}
	return dst
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
}

func resolveTint(spec tintSpec, grassCmap, foliageCmap image.Image) color.NRGBA {
	var cmap image.Image
	if spec.kind == "grass" {
		cmap = grassCmap
	} else if spec.kind == "foliage" {
		cmap = foliageCmap
	}
	if cmap == nil {
		return spec.fallback
	}
	b := cmap.Bounds()
	if b.Dx() < 2 || b.Dy() < 2 {
		return spec.fallback
	}
	t := clamp01(spec.temp)
	d := clamp01(spec.downfall) * t
	x := b.Min.X + int((1.0-t)*float64(b.Dx()-1))
	y := b.Min.Y + int((1.0-d)*float64(b.Dy()-1))
	c := color.NRGBAModel.Convert(cmap.At(x, y)).(color.NRGBA)
	if c.A == 0 {
		return spec.fallback
	}
	return c
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func applyMultiplyTint(img *image.NRGBA, tint color.NRGBA) *image.NRGBA {
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = uint8(int(img.Pix[i]) * int(tint.R) / 255)
		img.Pix[i+1] = uint8(int(img.Pix[i+1]) * int(tint.G) / 255)
		img.Pix[i+2] = uint8(int(img.Pix[i+2]) * int(tint.B) / 255)
	}
	return img
}

func compositeOver(dst, src *image.NRGBA) {
	w := min(dst.Bounds().Dx(), src.Bounds().Dx())
	h := min(dst.Bounds().Dy(), src.Bounds().Dy())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sc := src.NRGBAAt(x, y)
			if sc.A == 0 {
				continue
			}
			if sc.A == 255 {
				dst.SetNRGBA(x, y, sc)
				continue
			}
			dc := dst.NRGBAAt(x, y)
			sa := int(sc.A)
			inv := 255 - sa
			outA := sa + int(dc.A)*inv/255
			if outA == 0 {
				continue
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8((int(sc.R)*sa + int(dc.R)*int(dc.A)*inv/255) / outA),
				G: uint8((int(sc.G)*sa + int(dc.G)*int(dc.A)*inv/255) / outA),
				B: uint8((int(sc.B)*sa + int(dc.B)*int(dc.A)*inv/255) / outA),
				A: uint8(outA),
			})
		}
	}
}
