package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestExtractMessageFormatsRoles(t *testing.T) {
	got := ExtractMessage([]Message{
		{Role: "system", Content: "be nice"},
		{Role: "", Content: "  anonymous  "},
		{Role: "user", Content: "hello"},
		{Role: "user", Content: "   "},
	})
	want := "[system]: be nice\n\n[user]: anonymous\n\n[user]: hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestExtractMessageFromBlockContent(t *testing.T) {
	got := ExtractMessage([]Message{
		{Role: "user", Content: []any{
			map[string]any{"type": "text", "text": "  first  "},
			map[string]any{"type": "image_url"},
			map[string]any{"type": "text", "text": "second"},
		}},
	})
	if got != "[user]: first\nsecond" {
		t.Fatalf("unexpected block text: %q", got)
	}
}

func TestExtractMessageSkipsUnconvertibleContent(t *testing.T) {
	if got := ExtractMessage([]Message{{Role: "user", Content: 42}}); got != "" {
		t.Fatalf("expected empty for non-string content, got %q", got)
	}
}

func TestBuildGrokPayloadWithOverrides(t *testing.T) {
	temperature, topP, maxTokens := 0.4, 0.9, 512
	payload := BuildGrokPayload("hi", "expert", &temperature, &topP, &maxTokens)
	override, ok := payload["responseMetadata"].(map[string]any)["modelConfigOverride"].(map[string]any)
	if !ok {
		t.Fatalf("expected modelConfigOverride, got %#v", payload["responseMetadata"])
	}
	if override["temperature"] != 0.4 || override["topP"] != 0.9 || override["maxTokens"] != 512 {
		t.Fatalf("unexpected override: %#v", override)
	}
	if payload["modeId"] != "expert" || payload["message"] != "hi" {
		t.Fatalf("unexpected base payload: %#v", payload)
	}
}

func TestBuildGrokPayloadWithoutOverrides(t *testing.T) {
	payload := BuildGrokPayload("hi", "", nil, nil, nil)
	metadata := payload["responseMetadata"].(map[string]any)
	if _, exists := metadata["modelConfigOverride"]; exists {
		t.Fatalf("expected no override without tuning params, got %#v", metadata)
	}
}

func TestUpstreamErrorMessage(t *testing.T) {
	err := &UpstreamError{Status: 429, Message: "rate limited", Body: "raw"}
	if err.Error() != "rate limited" {
		t.Fatalf("expected message passthrough, got %q", err.Error())
	}
}

func TestParseUpstreamLineSkips(t *testing.T) {
	for _, line := range []string{"", "   ", "event: message", "not json", "[not json"} {
		events, err := ParseUpstreamLine(line)
		if err != nil || events != nil {
			t.Fatalf("line %q: expected nil,nil got %#v,%v", line, events, err)
		}
	}
}

func TestParseUpstreamLineDone(t *testing.T) {
	events, err := ParseUpstreamLine("data: [DONE]")
	if err != nil || len(events) != 1 || !events[0].SoftStop {
		t.Fatalf("expected soft stop for [DONE], got %#v,%v", events, err)
	}
}

func TestParseUpstreamLineTokenVariants(t *testing.T) {
	cases := map[string]UpstreamEvent{
		`{"result":{"response":{"token":"plain"}}}`:                            {Text: "plain"},
		`{"result":{"response":{"token":"thought","isThinking":true}}}`:        {Thinking: "thought"},
		`{"result":{"response":{"token":"tagged","messageTag":"final"}}}`:      {Text: "tagged"},
		`{"result":{"response":{"token":"skip","messageTag":"intermediate"}}}`: {},
	}
	for line, want := range cases {
		events, err := ParseUpstreamLine(line)
		if err != nil || len(events) != 1 {
			t.Fatalf("line %q: unexpected %#v,%v", line, events, err)
		}
		if events[0].Text != want.Text || events[0].Thinking != want.Thinking {
			t.Errorf("line %q: expected %#v, got %#v", line, want, events[0])
		}
	}
}

func TestParseUpstreamLineSoftStopTriggers(t *testing.T) {
	for _, line := range []string{
		`{"result":{"response":{"token":"t","isSoftStop":true}}}`,
		`{"result":{"response":{"token":"t","finalMetadata":{"a":1}}}}`,
	} {
		events, err := ParseUpstreamLine(line)
		if err != nil || len(events) != 1 || !events[0].SoftStop {
			t.Fatalf("line %q: expected soft stop, got %#v,%v", line, events, err)
		}
	}
}

func TestParseUpstreamLineErrors(t *testing.T) {
	cases := map[string]int{
		`{"error":{"message":"boom"}}`:                        502,
		`{"error":{"message":"too many requests"}}`:           429,
		`{"error":{"code":"8","message":"nope"}}`:             429,
		`{"error":{"error":"rate limit reached","code":"x"}}`: 429,
		`{"error":{}}`: 502,
	}
	for line, wantStatus := range cases {
		events, err := ParseUpstreamLine(line)
		if events != nil {
			t.Fatalf("line %q: expected no events, got %#v", line, events)
		}
		var upstream *UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != wantStatus {
			t.Fatalf("line %q: expected status %d, got %#v", line, wantStatus, err)
		}
		if upstream.Body != line {
			t.Errorf("line %q: expected raw body preserved, got %q", line, upstream.Body)
		}
	}
}

func TestScanUpstreamStreamsEvents(t *testing.T) {
	stream := strings.Join([]string{
		"event: message",
		`data: {"result":{"response":{"token":"a"}}}`,
		"",
		`data: {"result":{"response":{"token":"b"}}}`,
		"data: [DONE]",
	}, "\n")

	collected := make([]UpstreamEvent, 0)
	err := ScanUpstream(bufio.NewScanner(strings.NewReader(stream)), func(e UpstreamEvent) error {
		collected = append(collected, e)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected scan error: %v", err)
	}
	if len(collected) != 3 {
		t.Fatalf("expected 3 events, got %#v", collected)
	}
	if collected[0].Text != "a" || collected[1].Text != "b" || !collected[2].SoftStop {
		t.Fatalf("unexpected event order: %#v", collected)
	}
}

func TestScanUpstreamPropagatesErrors(t *testing.T) {
	boom := errors.New("callback failed")
	err := ScanUpstream(bufio.NewScanner(strings.NewReader("data: [DONE]")), func(UpstreamEvent) error {
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected callback error, got %v", err)
	}
}

func TestScanUpstreamStopsOnParseError(t *testing.T) {
	err := ScanUpstream(bufio.NewScanner(strings.NewReader("data: {\"error\":{}}\ndata: [DONE]")), func(UpstreamEvent) error {
		return nil
	})
	var upstream *UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestVideoSegmentLengths(t *testing.T) {
	cases := map[int][]int{6: {6}, 10: {10}, 12: {6, 6}, 16: {10, 6}, 20: {10, 10}}
	for seconds, want := range cases {
		got, ok := VideoSegmentLengths(seconds)
		if !ok || len(got) != len(want) {
			t.Fatalf("seconds %d: expected %v, got %v (ok=%v)", seconds, want, got, ok)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("seconds %d: expected %v, got %v", seconds, want, got)
			}
		}
	}
	if got, ok := VideoSegmentLengths(7); ok || got != nil {
		t.Fatalf("expected rejection for 7s, got %v (ok=%v)", got, ok)
	}
}

func TestBuildImagineRequest(t *testing.T) {
	request := BuildImagineRequest("req-1", "  a cat  ", "16:9", true, false)
	if request["type"] != "conversation.item.create" {
		t.Fatalf("unexpected type: %#v", request["type"])
	}
	content := request["item"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if content["requestId"] != "req-1" || content["text"] != "a cat" || content["type"] != "input_text" {
		t.Fatalf("unexpected content: %#v", content)
	}
	properties := content["properties"].(map[string]any)
	if properties["aspect_ratio"] != "16:9" || properties["enable_nsfw"] != true || properties["enable_pro"] != false {
		t.Fatalf("unexpected properties: %#v", properties)
	}
}

func TestParseMediaDataStreamingBlocks(t *testing.T) {
	raw := []byte(`{"result":{"response":{
		"streamingImageGenerationResponse":{"imageUrl":"https://img/1.png","imageId":"img1","progress":42,"imageIndex":3,"moderated":true},
		"streamingVideoGenerationResponse":{"videoUrl":"https://vid/1.mp4","videoId":"vid1","videoPostId":"post1","progress":80}
	}}}`)

	events := ParseMediaData(raw)
	if len(events) != 2 {
		t.Fatalf("expected image and video events, got %#v", events)
	}
	image := events[0]
	if image.Kind != "image" || image.URL != "https://img/1.png" || image.ImageID != "img1" || image.Progress != 42 || image.Index != 3 || !image.Moderated {
		t.Fatalf("unexpected image event: %#v", image)
	}
	video := events[1]
	if video.Kind != "video" || video.URL != "https://vid/1.mp4" || video.ImageID != "vid1" || video.PostID != "post1" || video.Progress != 80 {
		t.Fatalf("unexpected video event: %#v", video)
	}
}

func TestParseMediaDataModelResponse(t *testing.T) {
	raw := []byte(`{"modelResponse":{"generatedImageUrls":["a","","b"],"fileAttachments":["f1"]}}`)
	events := ParseMediaData(raw)
	if len(events) != 3 {
		t.Fatalf("expected 3 events (blank URL skipped), got %#v", events)
	}
	if events[0].URL != "a" || events[0].Progress != 100 || events[0].Index != 0 {
		t.Fatalf("unexpected first image: %#v", events[0])
	}
	if events[1].URL != "b" || events[1].Index != 2 {
		t.Fatalf("expected preserved index after blank skip: %#v", events[1])
	}
	if events[2].Kind != "asset" || events[2].AssetID != "f1" {
		t.Fatalf("unexpected asset event: %#v", events[2])
	}
}

func TestParseMediaDataPostAndCard(t *testing.T) {
	card, _ := json.Marshal(map[string]any{"image_chunk": map[string]any{"imageUrl": "https://card/i.png", "progress": 90, "moderated": true}})
	raw, _ := json.Marshal(map[string]any{
		"post":   map[string]any{"id": "post-1"},
		"result": map[string]any{"response": map[string]any{"cardAttachment": map[string]any{"jsonData": string(card)}}},
	})

	events := ParseMediaData(raw)
	if len(events) != 2 {
		t.Fatalf("expected post and card image events, got %#v", events)
	}
	if events[0].Kind != "post" || events[0].PostID != "post-1" {
		t.Fatalf("unexpected post event: %#v", events[0])
	}
	if events[1].Kind != "image" || events[1].URL != "https://card/i.png" || events[1].Progress != 90 || !events[1].Moderated {
		t.Fatalf("unexpected card event: %#v", events[1])
	}
}

func TestParseMediaDataRejectsInvalidJSON(t *testing.T) {
	if events := ParseMediaData([]byte("not json")); events != nil {
		t.Fatalf("expected nil for invalid JSON, got %#v", events)
	}
}

func TestParseSSELine(t *testing.T) {
	if kind, events := ParseSSELine("event: media"); kind != "" || events != nil {
		t.Fatalf("expected skip for event line, got %q,%#v", kind, events)
	}
	if kind, events := ParseSSELine("   "); kind != "" || events != nil {
		t.Fatalf("expected skip for blank line, got %q,%#v", kind, events)
	}
	if kind, events := ParseSSELine("plain text"); kind != "" || events != nil {
		t.Fatalf("expected skip for non-JSON, got %q,%#v", kind, events)
	}
	if kind, events := ParseSSELine("data: [DONE]"); kind != "done" || events != nil {
		t.Fatalf("expected done marker, got %q,%#v", kind, events)
	}
	kind, events := ParseSSELine(`data: {"modelResponse":{"generatedImageUrls":["z"]}}`)
	if kind != "data" || len(events) != 1 || events[0].URL != "z" {
		t.Fatalf("unexpected data parse: %q,%#v", kind, events)
	}
}

func TestParseDataURIValid(t *testing.T) {
	filename, mime, encoded, err := ParseDataURI("data:image/png;base64,aGVsbG8=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filename != "file.png" || mime != "image/png" || encoded != "aGVsbG8=" {
		t.Fatalf("unexpected parse: %q %q %q", filename, mime, encoded)
	}
}

func TestParseDataURINormalizesWhitespace(t *testing.T) {
	_, _, encoded, err := ParseDataURI("data:application/octet-stream;base64,aGVs\nbG8=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if encoded != "aGVsbG8=" {
		t.Fatalf("expected whitespace stripped, got %q", encoded)
	}
}

func TestParseDataURIErrors(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a.png": "URL",
		"data:image/png,notbase64":  "base64",
		"data:image/png;base64,###": "invalid base64",
	}
	for input, wantFragment := range cases {
		filename, _, encoded, err := ParseDataURI(input)
		if err == nil || !strings.Contains(err.Error(), wantFragment) {
			t.Fatalf("input %q: expected error containing %q, got %v", input, wantFragment, err)
		}
		if filename != "" || encoded != "" {
			t.Fatalf("input %q: expected empty outputs on error, got %q/%q", input, filename, encoded)
		}
	}

	// bare ";base64" with no MIME subtype falls back to the generic type; the
	// derived extension keeps the subtype name, so the filename is "file.octet-stream"
	filename, mime, _, err := ParseDataURI("data:;base64,aGVsbG8=")
	if err != nil || mime != "application/octet-stream" || filename != "file.octet-stream" {
		t.Fatalf("expected octet-stream fallback, got %q/%q (%v)", filename, mime, err)
	}
}

func TestStringFieldFallsBack(t *testing.T) {
	object := map[string]any{"first": "  ", "second": "  padded  "}
	if got := stringField(object, "first", "second"); got != "padded" {
		t.Fatalf("expected first non-blank trimmed value, got %q", got)
	}
	if got := stringField(object, "missing"); got != "" {
		t.Fatalf("expected empty for missing key, got %q", got)
	}
}

func TestIntFieldConversions(t *testing.T) {
	number := json.Number("7")
	object := map[string]any{
		"float":  12.9,
		"number": number,
		"int":    5,
		"string": "34",
		"bad":    "abc",
		"other":  []any{},
	}
	cases := map[string]int{"float": 12, "number": 7, "int": 5, "string": 34, "bad": 0, "other": 0, "missing": 0}
	for key, want := range cases {
		if got := intField(object, key); got != want {
			t.Errorf("key %q: expected %d, got %d", key, want, got)
		}
	}
}

func TestBoolField(t *testing.T) {
	object := map[string]any{"yes": true, "no": false, "text": "true"}
	if !boolField(object, "yes") || boolField(object, "no") || boolField(object, "text") || boolField(object, "missing") {
		t.Fatal("expected strict boolean coercion")
	}
}
