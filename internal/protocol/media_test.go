package protocol

import (
	"strings"
	"testing"
)

func TestAspectRatioValidOptions(t *testing.T) {
	cases := map[string]string{
		"1280x720": "16:9",
		"16:9":     "16:9",
		"720x1280": "9:16",
		"9:16":     "9:16",
		"1792x1024": "3:2",
		"3:2":      "3:2",
		"1024x1792": "2:3",
		"2:3":      "2:3",
		"1024x1024": "1:1",
		"1:1":      "1:1",
		"  16:9  ": "16:9", // trimmed case
	}
	for input, want := range cases {
		if got, ok := AspectRatio(input); !ok || got != want {
			t.Errorf("aspect %q: expected %q, got %q, ok=%v", input, want, got, ok)
		}
	}
}

func TestAspectRatioRejectsInvalid(t *testing.T) {
	for _, invalid := range []string{"", "invalid", "4:3", "100x200", "square"} {
		if got, ok := AspectRatio(invalid); ok || got != "" {
			t.Errorf("invalid %q: expected rejected, got %q, ok=%v", invalid, got, ok)
		}
	}
}

func TestBuildImageChatPayload(t *testing.T) {
	payload := BuildImageChatPayload("draw a cat", "grok-4.3", 2)
	if payload["imageGenerationCount"] != 2 {
		t.Fatalf("expected imageGenerationCount=2, got %#v", payload["imageGenerationCount"])
	}
}

func TestBuildVideoPayloadBasic(t *testing.T) {
	payload := BuildVideoPayload("prompt", "", "16:9", "720p", 10, "normal", nil)
	if payload["temporary"] != true || payload["modelName"] != "imagine-video-gen" {
		t.Fatalf("unexpected payload structure: %#v", payload)
	}
	if payload["message"] != "prompt --mode=normal" {
		t.Fatalf("unexpected message: %s", payload["message"])
	}
}

func TestBuildVideoPayloadPresetModes(t *testing.T) {
	cases := map[string]string{
		"fun":    "--mode=extremely-crazy",
		"normal": "--mode=normal",
		"spicy":  "--mode=extremely-spicy-or-crazy",
		"":       "--mode=custom",
	}
	for preset, wantMode := range cases {
		payload := BuildVideoPayload("p", "", "", "", 10, preset, nil)
		if !strings.Contains(payload["message"].(string), wantMode) {
			t.Errorf("preset %q: expected mode %q in message, got %q", preset, wantMode, payload["message"])
		}
	}
}

func TestBuildVideoPayloadWithImageRefs(t *testing.T) {
	payload := BuildVideoPayload("p", "parent", "", "", 10, "", []string{"ref1", "ref2"})
	config := payload["responseMetadata"].(map[string]any)["modelConfigOverride"].(map[string]any)["modelMap"].(map[string]any)["videoGenModelConfig"].(map[string]any)
	if config["isReferenceToVideo"] != true {
		t.Fatalf("expected isReferenceToVideo=true, got %#v", config["isReferenceToVideo"])
	}
	if config["isVideoEdit"] != false {
		t.Fatalf("expected isVideoEdit=false, got %#v", config["isVideoEdit"])
	}
	refs, ok := config["imageReferences"].([]string)
	if !ok || len(refs) != 2 || refs[0] != "ref1" || refs[1] != "ref2" {
		t.Fatalf("expected imageReferences []string{ref1 ref2}, got %#v", config["imageReferences"])
	}
}

func TestBuildVideoExtendPayloadModelNames(t *testing.T) {
	payload := BuildVideoExtendPayload("extend", "parent", "other", "16:9", "720p", 6, "high", 1.5)
	if payload["modelName"] != "imagine-video-gen" {
		t.Fatalf("expected imagine-video-gen, got %v", payload["modelName"])
	}
	config := payload["responseMetadata"].(map[string]any)["modelConfigOverride"].(map[string]any)["modelMap"].(map[string]any)["videoGenModelConfig"].(map[string]any)
	if config["isVideoExtension"] != true {
		t.Fatal("expected isVideoExtension=true")
	}
	if config["extendPostId"] != "other" {
		t.Fatalf("expected extendPostId=other, got %v", config["extendPostId"])
	}
	if config["originalRefType"] != "VIDEO" {
		t.Fatalf("expected originalRefType=VIDEO, got %v", config["originalRefType"])
	}
}

func TestBuildMediaPostPayload(t *testing.T) {
	payload := BuildMediaPostPayload(ImagePostMediaType, "https://img.png", "sunset")
	if payload["mediaType"] != ImagePostMediaType {
		t.Fatalf("expected mediaType %s, got %v", ImagePostMediaType, payload["mediaType"])
	}
	if payload["mediaUrl"] != "https://img.png" {
		t.Fatalf("unexpected mediaUrl: %v", payload["mediaUrl"])
	}
	if payload["prompt"] != "sunset" {
		t.Fatalf("unexpected prompt: %v", payload["prompt"])
	}
}

func TestBuildMediaPostPayloadSkipsEmptyURL(t *testing.T) {
	payload := BuildMediaPostPayload(ImagePostMediaType, "", "prompt")
	if _, ok := payload["mediaUrl"]; ok {
		t.Fatal("expected no mediaUrl for empty input")
	}
}

func TestBuildImageEditPayload(t *testing.T) {
	payload := BuildImageEditPayload("edit me", []string{"ref1"}, "parent")
	if payload["temporary"] != true {
		t.Fatal("expected temporary=true")
	}
	if payload["modelName"] != "imagine-image-edit" {
		t.Fatalf("unexpected modelName: %v", payload["modelName"])
	}
	refs := payload["responseMetadata"].(map[string]any)["modelConfigOverride"].(map[string]any)["modelMap"].(map[string]any)["imageEditModelConfig"].(map[string]any)["imageReferences"].([]string)
	if len(refs) != 1 || refs[0] != "ref1" {
		t.Fatalf("unexpected imageReferences: %v", refs)
	}
}

func TestBuildResetMessage(t *testing.T) {
	msg := BuildResetMessage()
	if msg["type"] != "conversation.item.create" {
		t.Fatalf("unexpected type: %v", msg["type"])
	}
	item := msg["item"].(map[string]any)
	if item["type"] != "message" {
		t.Fatalf("unexpected item type: %v", item["type"])
	}
	content := item["content"].([]any)[0].(map[string]any)
	if content["type"] != "reset" {
		t.Fatalf("unexpected content type: %v", content)
	}
}

func TestValidMediaFileID(t *testing.T) {
	valid := []string{"a1234567890abcdef", "ABCDEF12345678901234", "12345678901234567890123456789012345678901234567890ab"}
	invalid := []string{"", "abc", "12345-abcde", "not-hex", "12345678901234567890123456789012345678901234567890abcdefghi"} // too long
	for _, v := range valid {
		if !ValidMediaFileID(v) {
			t.Errorf("expected valid: %s", v)
		}
	}
	for _, inv := range invalid {
		if ValidMediaFileID(inv) {
			t.Errorf("expected invalid: %s", inv)
		}
	}
}

func TestResolveDownloadURLStandard(t *testing.T) {
	got, scheme := ResolveDownloadURL("https://host.example/path/file.mp4")
	if got != "https://host.example/path/file.mp4" || scheme != "https://host.example" {
		t.Fatalf("unexpected resolution: %s, %s", got, scheme)
	}
}

func TestResolveDownloadURLRelative(t *testing.T) {
	got, scheme := ResolveDownloadURL("/relative/path.mp4")
	if got != "https://assets.grok.com/relative/path.mp4" || scheme != "https://assets.grok.com" {
		t.Fatalf("unexpected relative resolution: %s, %s", got, scheme)
	}
}

func TestResolveDownloadURLLeadingSlash(t *testing.T) {
	got, _ := ResolveDownloadURL("relative/path.mp4")
	if got != "https://assets.grok.com/relative/path.mp4" {
		t.Fatalf("unexpected leading-slash handling: %s", got)
	}
}

func TestResolveAssetReferenceByURI(t *testing.T) {
	if got := ResolveAssetReference("", "https://host/xyz", ""); got != "https://host/xyz" {
		t.Fatalf("expected URI return: %s", got)
	}
}

func TestResolveAssetReferenceByFileID(t *testing.T) {
	if got := ResolveAssetReference("file123", "", "user42"); got != "https://assets.grok.com/users/user42/file123/content" {
		t.Fatalf("expected fileID+userID return: %s", got)
	}
}

func TestResolveAssetReferenceEmpty(t *testing.T) {
	if got := ResolveAssetReference("", "", ""); got != "" {
		t.Fatalf("expected empty for no identifiers: %s", got)
	}
}

func TestMediaFileIDPatternMatches(t *testing.T) {
	for _, s := range []string{"aabbccddeeff1122", "12345678-1234-1234-1234-123456789012"} {
		if !mediaFileIDPattern.MatchString(s) {
			t.Errorf("expected pattern to match: %s", s)
		}
	}
}