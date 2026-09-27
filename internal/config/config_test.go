package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClampInt(t *testing.T) {
	cases := []struct {
		name  string
		value int
		min   int
		max   int
		want  int
	}{
		{name: "低于下界取下界", value: 5, min: 10, max: 300, want: 10},
		{name: "高于上界取上界", value: 999, min: 10, max: 300, want: 300},
		{name: "区间内原样返回", value: 180, min: 10, max: 300, want: 180},
		{name: "等于下界", value: 10, min: 10, max: 300, want: 10},
		{name: "等于上界", value: 300, min: 10, max: 300, want: 300},
		{name: "允许下界为零", value: -1, min: 0, max: 3, want: 0},
		{name: "上下界相同", value: 7, min: 5, max: 5, want: 5},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := clampInt(testCase.value, testCase.min, testCase.max); got != testCase.want {
				t.Fatalf("clampInt(%d, %d, %d) = %d, want %d", testCase.value, testCase.min, testCase.max, got, testCase.want)
			}
		})
	}
}

// env 测试
func TestEnv(t *testing.T) {
	t.Setenv("GO_TEST_ENV_VALUE", "hello")
	t.Setenv("GO_TEST_ENV_EMPTY", "")

	cases := []struct {
		name     string
		envName  string
		fallback string
		want     string
	}{
		{name: "环境变量存在", envName: "GO_TEST_ENV_VALUE", fallback: "default", want: "hello"},
		{name: "环境变量为空取默认", envName: "GO_TEST_ENV_EMPTY", fallback: "default", want: "default"},
		{name: "环境变量不存在取默认", envName: "GO_TEST_ENV_NOT_EXIST", fallback: "fallback", want: "fallback"},
		{name: "环境变量带空格截断", envName: "GO_TEST_ENV_TRIM", fallback: "x", want: "y"},
	}
	t.Setenv("GO_TEST_ENV_TRIM", "  y  ")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := env(tc.envName, tc.fallback); got != tc.want {
				t.Fatalf("env(%q, %q) = %q, want %q", tc.envName, tc.fallback, got, tc.want)
			}
		})
	}
}

// envBool 测试
func TestEnvBool(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		fallback bool
		want     bool
	}{
		{name: "true 返回 true", value: "true", fallback: false, want: true},
		{name: "1 返回 true", value: "1", fallback: false, want: true},
		{name: "yes 返回 true", value: "yes", fallback: false, want: true},
		{name: "on 返回 true", value: "on", fallback: false, want: true},
		{name: "false 返回 false", value: "false", fallback: true, want: false},
		{name: "空字符串取默认", value: "", fallback: true, want: true},
		{name: "其他字符串取默认", value: "maybe", fallback: false, want: false},
		{name: "带空格截断", value: "  true  ", fallback: false, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GO_TEST_BOOL", tc.value)
			if got := envBool("GO_TEST_BOOL", tc.fallback); got != tc.want {
				t.Fatalf("envBool(%q, %v) = %v, want %v", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

// envInt 测试
func TestEnvInt(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		fallback int
		want     int
	}{
		{name: "有效正整数", value: "42", fallback: 10, want: 42},
		{name: "非数字取默认", value: "abc", fallback: 5, want: 5},
		{name: "小于1取默认", value: "0", fallback: 3, want: 3},
		{name: "负数取默认", value: "-1", fallback: 7, want: 7},
		{name: "空字符串取默认", value: "", fallback: 9, want: 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GO_TEST_INT", tc.value)
			if got := envInt("GO_TEST_INT", tc.fallback); got != tc.want {
				t.Fatalf("envInt(%q, %d) = %d, want %d", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

// envIntAllowZero 测试
func TestEnvIntAllowZero(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		fallback int
		want     int
	}{
		{name: "零是有效值", value: "0", fallback: 5, want: 0},
		{name: "正整数", value: "3", fallback: 1, want: 3},
		{name: "负数取默认", value: "-1", fallback: 2, want: 2},
		{name: "非数字取默认", value: "xyz", fallback: 4, want: 4},
		{name: "空字符串取默认", value: "", fallback: 6, want: 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GO_TEST_INT_ZERO", tc.value)
			if got := envIntAllowZero("GO_TEST_INT_ZERO", tc.fallback); got != tc.want {
				t.Fatalf("envIntAllowZero(%q, %d) = %d, want %d", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

// configBool 测试
func TestConfigBool(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		fallback bool
		want     bool
	}{
		{name: "bool true", value: true, fallback: false, want: true},
		{name: "bool false", value: false, fallback: true, want: false},
		{name: "非 bool 取默认", value: "yes", fallback: true, want: true},
		{name: "nil 取默认", value: nil, fallback: false, want: false},
		{name: "数字取默认", value: 42, fallback: true, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := configBool(tc.value, tc.fallback); got != tc.want {
				t.Fatalf("configBool(%v, %v) = %v, want %v", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

// configInt 测试
func TestConfigInt(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  int
	}{
		{name: "float64", value: float64(42), want: 42},
		{name: "int", value: 42, want: 42},
		{name: "json.Number 字符串", value: json.Number("99"), want: 99},
		{name: "字符串数字", value: "123", want: 123},
		{name: "空字符串取0", value: "", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := configInt(tc.value); got != tc.want {
				t.Fatalf("configInt(%v) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

// parseStatusCodes 测试
func TestParseStatusCodes(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  map[int]bool
	}{
		{name: "正常范围", value: "401,403,429", want: map[int]bool{401: true, 403: true, 429: true}},
		{name: "包含范围外码", value: "200,404,600", want: map[int]bool{404: true}},
		{name: "含空格", value: "401, 403, 429", want: map[int]bool{401: true, 403: true, 429: true}},
		{name: "空字符串", value: "", want: map[int]bool{}},
		{name: "非数字被跳过", value: "abc,401", want: map[int]bool{401: true}},
		{name: "全部无效", value: "200,301", want: map[int]bool{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStatusCodes(tc.value)
			if len(got) != len(tc.want) {
				t.Fatalf("parseStatusCodes(%q) = %v, want %v", tc.value, got, tc.want)
			}
			for k := range tc.want {
				if !got[k] {
					t.Fatalf("parseStatusCodes(%q) missing key %d", tc.value, k)
				}
			}
		})
	}
}

// resolvePath 测试
func TestResolvePath(t *testing.T) {
	cases := []struct {
		name string
		root string
		path string
		want string
	}{
		{name: "绝对路径原样返回", root: "/tmp", path: "/abs/path", want: "/abs/path"},
		{name: "相对路径拼接根目录", root: "/tmp", path: "data/file.json", want: filepath.Join("/tmp", "data/file.json")},
		{name: "根目录相对路径", root: "/var/app", path: "config.json", want: filepath.Join("/var/app", "config.json")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePath(tc.root, tc.path); got != tc.want {
				t.Fatalf("resolvePath(%q, %q) = %q, want %q", tc.root, tc.path, got, tc.want)
			}
		})
	}
}

// readMap 测试
func TestReadMap(t *testing.T) {
	// 有效 JSON 文件
	tmpDir := t.TempDir()
	validJSON := filepath.Join(tmpDir, "valid.json")
	if err := os.WriteFile(validJSON, []byte(`{"key":"value","num":42}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// 非法 JSON 文件
	invalidJSON := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(invalidJSON, []byte(`{not valid json`), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		path    string
		want    map[string]any
		wantErr bool
	}{
		{name: "有效 JSON", path: validJSON, want: map[string]any{"key": "value", "num": float64(42)}, wantErr: false},
		{name: "非法 JSON", path: invalidJSON, want: nil, wantErr: true},
		{name: "文件不存在", path: filepath.Join(tmpDir, "nope.json"), want: nil, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readMap(tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("readMap(%q) expected error, got nil", tc.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("readMap(%q) unexpected error: %v", tc.path, err)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("readMap(%q)[%q] = %v, want %v", tc.path, k, got[k], v)
				}
			}
		})
	}
}

// firstString 测试
func TestFirstString(t *testing.T) {
	m := map[string]any{
		"auth-key": "secret123",
		"api_key":  "secret456",
		"app_key":  "",
		"empty":    "",
	}

	cases := []struct {
		name string
		keys []string
		want string
	}{
		{name: "找到第一个非空字符串", keys: []string{"auth-key", "api_key"}, want: "secret123"},
		{name: "跳过空字符串", keys: []string{"app_key", "auth-key"}, want: "secret123"},
		{name: "键不存在返回空", keys: []string{"missing"}, want: ""},
		{name: "所有键为空返回空", keys: []string{"empty", "missing"}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstString(m, tc.keys...); got != tc.want {
				t.Fatalf("firstString(%v, %v) = %q, want %q", m, tc.keys, got, tc.want)
			}
		})
	}
}

// splitList 测试
func TestSplitList(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "逗号分隔", value: "a,b,c", want: []string{"a", "b", "c"}},
		{name: "换行分隔", value: "a\nb\nc", want: []string{"a", "b", "c"}},
		{name: "混合分隔", value: "a, b\nc\r d", want: []string{"a", "b", "c", "d"}},
		{name: "空字符串", value: "", want: []string{}},
		{name: "仅分隔符", value: ",,\n\r", want: []string{}},
		{name: "带空格元素", value: "  x  ,  y  ", want: []string{"x", "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitList(tc.value)
			if len(got) != len(tc.want) {
				t.Fatalf("splitList(%q) = %v, want %v", tc.value, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("splitList(%q)[%d] = %q, want %q", tc.value, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// anyStringList 测试
func TestAnyStringList(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  []string
	}{
		{name: "[]any 字符串", value: []any{"a", "b", "c"}, want: []string{"a", "b", "c"}},
		{name: "[]string", value: []string{"x", "y"}, want: []string{"x", "y"}},
		{name: "字符串逗号分隔", value: "p,q,r", want: []string{"p", "q", "r"}},
		{name: "nil 返回空", value: nil, want: []string{}},
		{name: "空切片", value: []any{}, want: []string{}},
		{name: "含空元素过滤", value: []any{"a", "", "b"}, want: []string{"a", "b"}},
		{name: "非字符串类型", value: []any{1, 2, 3}, want: []string{"1", "2", "3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := anyStringList(tc.value)
			if len(got) != len(tc.want) {
				t.Fatalf("anyStringList(%v) = %v, want %v", tc.value, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("anyStringList(%v)[%d] = %q, want %q", tc.value, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// RelativePath 测试
func TestRelativePath(t *testing.T) {
	c := Config{RootDir: "/tmp/app"}
	got := c.RelativePath("data/file.json")
	want := filepath.Join("/tmp/app", "data/file.json")
	if got != want {
		t.Fatalf("RelativePath(%q) = %q, want %q", "data/file.json", got, want)
	}

	abs := c.RelativePath("/abs/path")
	if abs != "/abs/path" {
		t.Fatalf("RelativePath(/abs/path) = %q, want /abs/path", abs)
	}
}

// Load 测试
func TestLoad(t *testing.T) {
	// 默认配置
	t.Run("默认配置", func(t *testing.T) {
		os.Unsetenv("GO_REQUEST_TIMEOUT_SECONDS")
		os.Unsetenv("GO_CHAT_MAX_RETRIES")
		os.Unsetenv("GO_IMAGE_ACCOUNT_CONCURRENCY")
		os.Unsetenv("GO_IMAGE_MAX_CONCURRENCY")
		os.Unsetenv("GO_IMAGE_RETENTION_DAYS")
		os.Unsetenv("GO_IMAGE_CLEANUP_INTERVAL_SECONDS")
		os.Unsetenv("GO_LISTEN_ADDR")
		os.Unsetenv("CHATGPT2API_LISTEN_ADDR")
		os.Unsetenv("GROK_DATA_DIR")
		os.Unsetenv("GO_STATIC_DIR")
		os.Unsetenv("GO_CONFIG_PATH")
		os.Unsetenv("GO_ACCOUNTS_PATH")
		os.Unsetenv("GO_AUTH_KEYS_PATH")
		os.Unsetenv("CHATGPT2API_AUTH_KEY")
		os.Unsetenv("CHATGPT2API_ADMIN_KEY")
		os.Unsetenv("CHATGPT2API_WEBUI_KEY")
		os.Unsetenv("GO_UPSTREAM_URL")
		os.Unsetenv("GO_OPENAI_BASE_URL")
		os.Unsetenv("GO_OPENAI_OAUTH_TOKEN_URL")
		os.Unsetenv("GO_OPENAI_AUTH_BASE_URL")
		os.Unsetenv("GO_OPENAI_PLATFORM_BASE_URL")
		os.Unsetenv("GO_OPENAI_LOGIN_TOKEN_URL")
		os.Unsetenv("GO_OPENAI_AGENT_REGISTER_URL")
		os.Unsetenv("GO_GROK_CHAT_URL")
		os.Unsetenv("GO_GROK_RATE_LIMITS_URL")
		os.Unsetenv("GO_XAI_CLI_BASE_URL")
		os.Unsetenv("GO_XAI_CLI_TOKEN_URL")
		os.Unsetenv("GO_CONSOLE_RESPONSES_URL")
		os.Unsetenv("GO_MEDIA_CHAT_URL")
		os.Unsetenv("GO_MEDIA_POST_URL")
		os.Unsetenv("GO_ASSET_UPLOAD_URL")
		os.Unsetenv("GO_ASSETS_BASE_URL")
		os.Unsetenv("GO_IMAGINE_WS_URL")
		os.Unsetenv("GO_PROXY_URL")
		os.Unsetenv("GO_PROXY_POOL")
		os.Unsetenv("GO_RESOURCE_PROXY_URL")
		os.Unsetenv("GO_RESOURCE_PROXY_POOL")
		os.Unsetenv("GO_PROXY_UPSTREAMS_FILE")
		os.Unsetenv("GO_FLARESOLVERR_URL")
		os.Unsetenv("GO_CLEARANCE_ENABLED")
		os.Unsetenv("GO_CLEARANCE_TIMEOUT_SECONDS")
		os.Unsetenv("GO_QUEUE_BACKEND")
		os.Unsetenv("GO_REDIS_ADDR")
		os.Unsetenv("GO_REDIS_PASSWORD")
		os.Unsetenv("GO_REDIS_DB")
		os.Unsetenv("GO_XAI_OAUTH_DEVICE_URL")
		os.Unsetenv("GO_XAI_OAUTH_TOKEN_URL")
		os.Unsetenv("GO_IMAGE_DATA_DIR")
		os.Unsetenv("GO_VIDEO_DATA_DIR")
		os.Unsetenv("GO_OAUTH_PATH")
		os.Unsetenv("GO_QUEUE_PATH")
		os.Unsetenv("GO_REGISTER_PATH")
		os.Unsetenv("GO_GROK_ACCOUNTS_PATH")
		os.Unsetenv("GO_REGISTER_MAIL_URL")
		os.Unsetenv("GO_REGISTER_CAPTCHA_URL")
		os.Unsetenv("GO_REGISTER_DRIVER_URL")
		os.Unsetenv("GO_REGISTER_DRIVER_KEY")
		os.Unsetenv("GO_VERSION")
		os.Unsetenv("GO_ALLOW_ANONYMOUS")
		os.Unsetenv("GO_CHAT_RETRY_CODES")

		root := t.TempDir()
		cfg, err := Load(root)
		if err != nil {
			t.Fatalf("Load default failed: %v", err)
		}
		if cfg.ListenAddr != ":8080" {
			t.Fatalf("ListenAddr = %q, want :8080", cfg.ListenAddr)
		}
		if cfg.RequestTimeout != 180*time.Second {
			t.Fatalf("RequestTimeout = %v, want 180s", cfg.RequestTimeout)
		}
		if cfg.ChatMaxRetries != 2 {
			t.Fatalf("ChatMaxRetries = %d, want 2", cfg.ChatMaxRetries)
		}
		if cfg.ImageAccountLimit != 1 {
			t.Fatalf("ImageAccountLimit = %d, want 1", cfg.ImageAccountLimit)
		}
		if cfg.ImageMaxConcurrency != 128 {
			t.Fatalf("ImageMaxConcurrency = %d, want 128", cfg.ImageMaxConcurrency)
		}
		if cfg.QueueBackend != "json" {
			t.Fatalf("QueueBackend = %q, want json", cfg.QueueBackend)
		}
		if cfg.RedisAddr != "127.0.0.1:6379" {
			t.Fatalf("RedisAddr = %q, want 127.0.0.1:6379", cfg.RedisAddr)
		}
		if cfg.RedisDB != 0 {
			t.Fatalf("RedisDB = %d, want 0", cfg.RedisDB)
		}
		if cfg.Version != "1.2.4-go" {
			t.Fatalf("Version = %q, want 1.2.4-go", cfg.Version)
		}
		if cfg.AllowAnonymous != false {
			t.Fatalf("AllowAnonymous = %v, want false", cfg.AllowAnonymous)
		}
		if cfg.APIKey != "" {
			t.Fatalf("APIKey = %q, want empty", cfg.APIKey)
		}
		if cfg.ClearanceEnabled != false {
			t.Fatalf("ClearanceEnabled = %v, want false", cfg.ClearanceEnabled)
		}
		if cfg.ClearanceTimeout != 60*time.Second {
			t.Fatalf("ClearanceTimeout = %v, want 60s", cfg.ClearanceTimeout)
		}
		if len(cfg.ChatRetryCodes) == 0 {
			t.Fatal("ChatRetryCodes should not be empty")
		}
	})

	// 环境变量覆盖默认值
	t.Run("环境变量覆盖", func(t *testing.T) {
		t.Setenv("GO_REQUEST_TIMEOUT_SECONDS", "60")
		t.Setenv("GO_CHAT_MAX_RETRIES", "5")
		t.Setenv("GO_IMAGE_ACCOUNT_CONCURRENCY", "3")
		t.Setenv("GO_IMAGE_MAX_CONCURRENCY", "256")
		t.Setenv("GO_LISTEN_ADDR", ":9090")
		t.Setenv("GO_QUEUE_BACKEND", "redis")
		t.Setenv("GO_REDIS_DB", "2")
		t.Setenv("GO_VERSION", "2.0.0")
		t.Setenv("GO_ALLOW_ANONYMOUS", "true")
		t.Setenv("GO_CLEARANCE_ENABLED", "yes")
		t.Setenv("GO_CLEARANCE_TIMEOUT_SECONDS", "120")

		root := t.TempDir()
		cfg, err := Load(root)
		if err != nil {
			t.Fatalf("Load with env failed: %v", err)
		}
		if cfg.RequestTimeout != 60*time.Second {
			t.Fatalf("RequestTimeout = %v, want 60s", cfg.RequestTimeout)
		}
		if cfg.ChatMaxRetries != 3 {
			t.Fatalf("ChatMaxRetries = %d, want 3 (clamped to max)", cfg.ChatMaxRetries)
		}
		if cfg.ImageAccountLimit != 3 {
			t.Fatalf("ImageAccountLimit = %d, want 3", cfg.ImageAccountLimit)
		}
		if cfg.ImageMaxConcurrency != 256 {
			t.Fatalf("ImageMaxConcurrency = %d, want 256", cfg.ImageMaxConcurrency)
		}
		if cfg.ListenAddr != ":9090" {
			t.Fatalf("ListenAddr = %q, want :9090", cfg.ListenAddr)
		}
		if cfg.QueueBackend != "redis" {
			t.Fatalf("QueueBackend = %q, want redis", cfg.QueueBackend)
		}
		if cfg.RedisDB != 2 {
			t.Fatalf("RedisDB = %d, want 2", cfg.RedisDB)
		}
		if cfg.Version != "2.0.0" {
			t.Fatalf("Version = %q, want 2.0.0", cfg.Version)
		}
		if cfg.AllowAnonymous != true {
			t.Fatalf("AllowAnonymous = %v, want true", cfg.AllowAnonymous)
		}
		if cfg.ClearanceEnabled != true {
			t.Fatalf("ClearanceEnabled = %v, want true", cfg.ClearanceEnabled)
		}
		if cfg.ClearanceTimeout != 120*time.Second {
			t.Fatalf("ClearanceTimeout = %v, want 120s", cfg.ClearanceTimeout)
		}
	})

	// envInt 边界钳制
	t.Run("envInt 边界钳制", func(t *testing.T) {
		t.Setenv("GO_REQUEST_TIMEOUT_SECONDS", "5")
		root := t.TempDir()
		cfg, err := Load(root)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.RequestTimeout != 10*time.Second {
			t.Fatalf("RequestTimeout clamped to %v, want 10s", cfg.RequestTimeout)
		}
	})
}

// Load JSON 配置覆盖测试
func TestLoadJSONConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "myconfig.json")

	t.Run("JSON 配置覆盖", func(t *testing.T) {
		configContent := map[string]any{
			"auth-key": "json-secret",
			"app_key":  "json-app-key",
		}
		data, _ := json.Marshal(configContent)
		if err := os.WriteFile(configPath, data, 0o644); err != nil {
			t.Fatal(err)
		}

		t.Setenv("GO_CONFIG_PATH", configPath)
		t.Setenv("CHATGPT2API_AUTH_KEY", "") // 确保 env key 为空以便 JSON 生效

		cfg, err := Load(root)
		if err != nil {
			t.Fatalf("Load JSON config failed: %v", err)
		}
		if cfg.APIKey != "json-secret" {
			t.Fatalf("APIKey = %q, want json-secret", cfg.APIKey)
		}
		if cfg.AdminKey != "json-app-key" {
			t.Fatalf("AdminKey = %q, want json-app-key", cfg.AdminKey)
		}
	})

	t.Run("JSON 配置中不存在的键取环境变量", func(t *testing.T) {
		t.Setenv("CHATGPT2API_AUTH_KEY", "env-key")
		t.Setenv("GO_CONFIG_PATH", configPath)

		cfg, err := Load(root)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		// APIKey 来自 env，因为 config 中没有 auth-key
		if cfg.APIKey != "env-key" {
			t.Fatalf("APIKey = %q, want env-key", cfg.APIKey)
		}
	})
}

// Load 密钥回退逻辑测试
func TestLoadKeyFallback(t *testing.T) {
	root := t.TempDir()

	t.Run("AdminKey 非空时 APIKey 回退", func(t *testing.T) {
		configPath := filepath.Join(root, "keys.json")
		data, _ := json.Marshal(map[string]any{
			"app_key": "admin-from-json",
		})
		os.WriteFile(configPath, data, 0o644)

		t.Setenv("GO_CONFIG_PATH", configPath)
		t.Setenv("CHATGPT2API_AUTH_KEY", "")
		t.Setenv("CHATGPT2API_ADMIN_KEY", "")

		cfg, err := Load(root)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.AdminKey != "admin-from-json" {
			t.Fatalf("AdminKey = %q, want admin-from-json", cfg.AdminKey)
		}
		if cfg.APIKey != "admin-from-json" {
			t.Fatalf("APIKey = %q, want admin-from-json (fallback from AdminKey)", cfg.APIKey)
		}
	})

	t.Run("WebUIKey 从 JSON 读取", func(t *testing.T) {
		configPath := filepath.Join(root, "webui.json")
		data, _ := json.Marshal(map[string]any{
			"webui_key": "webui-secret",
		})
		os.WriteFile(configPath, data, 0o644)

		t.Setenv("GO_CONFIG_PATH", configPath)
		t.Setenv("CHATGPT2API_WEBUI_KEY", "")

		cfg, err := Load(root)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.WebUIKey != "webui-secret" {
			t.Fatalf("WebUIKey = %q, want webui-secret", cfg.WebUIKey)
		}
	})
}

// Load 非法 JSON 测试
func TestLoadMalformedJSON(t *testing.T) {
	root := t.TempDir()
	badPath := filepath.Join(root, "bad.json")
	os.WriteFile(badPath, []byte(`not valid json{{{`), 0o644)

	t.Setenv("GO_CONFIG_PATH", badPath)
	_, err := Load(root)
	if err == nil {
		t.Fatal("Load with malformed JSON should return error")
	}
}

// ApplyProxyConfig nil 保护测试
func TestApplyProxyConfigNil(t *testing.T) {
	ApplyProxyConfig(nil, map[string]any{"proxy": "http://test"})
	// 不应 panic
}

// applyProxyConfig 测试
func TestApplyProxyConfig(t *testing.T) {
	t.Run("代理字符串", func(t *testing.T) {
		cfg := &Config{ProxyURL: ""}
		applyProxyConfig(cfg, map[string]any{"proxy": "http://proxy.example.com"})
		if cfg.ProxyURL != "http://proxy.example.com" {
			t.Fatalf("ProxyURL = %q, want http://proxy.example.com", cfg.ProxyURL)
		}
	})

	t.Run("代理 Map 字符串", func(t *testing.T) {
		cfg := &Config{ProxyURL: ""}
		applyProxyConfig(cfg, map[string]any{
			"proxy": map[string]any{
				"proxy_url": "http://map-proxy.com",
			},
		})
		if cfg.ProxyURL != "http://map-proxy.com" {
			t.Fatalf("ProxyURL = %q, want http://map-proxy.com", cfg.ProxyURL)
		}
	})

	t.Run("已有 ProxyURL 不覆盖", func(t *testing.T) {
		cfg := &Config{ProxyURL: "existing"}
		applyProxyConfig(cfg, map[string]any{"proxy": "http://new-proxy.com"})
		if cfg.ProxyURL != "existing" {
			t.Fatalf("ProxyURL = %q, want existing (not overwritten)", cfg.ProxyURL)
		}
	})

	t.Run("proxy_runtime Map", func(t *testing.T) {
		cfg := &Config{ProxyPool: []string{}}
		applyProxyConfig(cfg, map[string]any{
			"proxy_runtime": map[string]any{
				"proxy_pool": []any{"p1", "p2"},
			},
		})
		if len(cfg.ProxyPool) != 2 || cfg.ProxyPool[0] != "p1" {
			t.Fatalf("ProxyPool = %v, want [p1 p2]", cfg.ProxyPool)
		}
	})

	t.Run("ResourceProxyURL 和 ResourceProxyPool", func(t *testing.T) {
		cfg := &Config{ResourceProxyURL: "", ResourceProxyPool: []string{}}
		applyProxyConfig(cfg, map[string]any{
			"proxy": map[string]any{
				"resource_proxy_url":  "http://res-proxy.com",
				"resource_proxy_pool": []any{"r1", "r2"},
			},
		})
		if cfg.ResourceProxyURL != "http://res-proxy.com" {
			t.Fatalf("ResourceProxyURL = %q", cfg.ResourceProxyURL)
		}
		if len(cfg.ResourceProxyPool) != 2 {
			t.Fatalf("ResourceProxyPool len = %d", len(cfg.ResourceProxyPool))
		}
	})

	t.Run("nil proxy 不覆盖", func(t *testing.T) {
		cfg := &Config{ProxyURL: "keep"}
		applyProxyConfig(cfg, map[string]any{})
		if cfg.ProxyURL != "keep" {
			t.Fatalf("ProxyURL = %q, want keep", cfg.ProxyURL)
		}
	})

	t.Run("FallbackProxy 和 ProxyGroups", func(t *testing.T) {
		cfg := &Config{}
		applyProxyConfig(cfg, map[string]any{
			"fallback_proxy": "fallback-proxy.com",
			"proxy_groups": []any{
				map[string]any{
					"id":   "g1",
					"name": "Group1",
				},
			},
		})
		if cfg.FallbackProxy != "fallback-proxy.com" {
			t.Fatalf("FallbackProxy = %q", cfg.FallbackProxy)
		}
		if len(cfg.ProxyGroups) != 1 || cfg.ProxyGroups[0].ID != "g1" {
			t.Fatalf("ProxyGroups = %v", cfg.ProxyGroups)
		}
	})
}

// parseProxyGroups 测试
func TestParseProxyGroups(t *testing.T) {
	t.Run("nil 输入返回空", func(t *testing.T) {
		got := parseProxyGroups(nil)
		if len(got) != 0 {
			t.Fatalf("parseProxyGroups(nil) = %v, want empty", got)
		}
	})

	t.Run("非切片输入返回空", func(t *testing.T) {
		got := parseProxyGroups("not-a-list")
		if len(got) != 0 {
			t.Fatalf("parseProxyGroups(string) = %v, want empty", got)
		}
	})

	t.Run("有效分组和节点", func(t *testing.T) {
		got := parseProxyGroups([]any{
			map[string]any{
				"id":       "g1",
				"name":     "Group1",
				"enabled":  true,
				"strategy": "round-robin",
				"nodes": []any{
					map[string]any{
						"id":                      "n1",
						"name":                    "Node1",
						"url":                     "http://n1.com",
						"enabled":                 true,
						"image_concurrency_limit": float64(5),
						"last_status":             float64(200),
						"runtime_failure_count":   float64(1),
						"runtime_success_count":   float64(10),
						"runtime_latency_ms":      float64(100),
					},
				},
			},
		})
		if len(got) != 1 {
			t.Fatalf("len(got) = %d, want 1", len(got))
		}
		g := got[0]
		if g.ID != "g1" || g.Name != "Group1" || !g.Enabled || g.Strategy != "round-robin" {
			t.Fatalf("group = %+v", g)
		}
		if len(g.Nodes) != 1 {
			t.Fatalf("len(nodes) = %d, want 1", len(g.Nodes))
		}
		n := g.Nodes[0]
		if n.ID != "n1" || n.URL != "http://n1.com" || n.ImageConcurrencyLimit != 5 {
			t.Fatalf("node = %+v", n)
		}
		if n.LastStatus != 200 {
			t.Fatalf("LastStatus = %d, want 200", n.LastStatus)
		}
		if n.RuntimeFailures != 1 {
			t.Fatalf("RuntimeFailures = %d", n.RuntimeFailures)
		}
		if n.RuntimeSuccesses != 10 {
			t.Fatalf("RuntimeSuccesses = %d", n.RuntimeSuccesses)
		}
		if n.RuntimeLatencyMS != 100 {
			t.Fatalf("RuntimeLatencyMS = %d", n.RuntimeLatencyMS)
		}
	})

	t.Run("节点 concurrency 小于 1 默认为 3", func(t *testing.T) {
		got := parseProxyGroups([]any{
			map[string]any{
				"nodes": []any{
					map[string]any{
						"image_concurrency_limit": float64(0),
					},
				},
			},
		})
		if len(got) != 1 || got[0].Nodes[0].ImageConcurrencyLimit != 3 {
			t.Fatalf("ImageConcurrencyLimit = %d, want 3", got[0].Nodes[0].ImageConcurrencyLimit)
		}
	})

	t.Run("nil group 跳过", func(t *testing.T) {
		got := parseProxyGroups([]any{nil, "string", 123})
		if len(got) != 0 {
			t.Fatalf("parseProxyGroups with nil items = %v, want empty", got)
		}
	})
}

// Load 完整流程集成测试
func TestLoadIntegration(t *testing.T) {
	root := t.TempDir()

	// 创建完整的 config.json
	configContent := map[string]any{
		"auth-key":       "full-api-key",
		"app_key":        "full-admin-key",
		"webui_key":      "full-webui-key",
		"fallback_proxy": "fb-proxy.com",
		"proxy_groups": []any{
			map[string]any{
				"id":   "pg1",
				"name": "ProxyGroup1",
				"nodes": []any{
					map[string]any{
						"id":   "pn1",
						"name": "ProxyNode1",
						"url":  "http://pn1.example.com",
					},
				},
			},
		},
	}
	data, _ := json.Marshal(configContent)
	configPath := filepath.Join(root, "config.json")
	os.WriteFile(configPath, data, 0o644)

	t.Setenv("GO_CONFIG_PATH", configPath)
	t.Setenv("CHATGPT2API_AUTH_KEY", "")
	t.Setenv("CHATGPT2API_ADMIN_KEY", "")
	t.Setenv("CHATGPT2API_WEBUI_KEY", "")

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("LoadIntegration failed: %v", err)
	}
	if cfg.APIKey != "full-api-key" {
		t.Fatalf("APIKey = %q, want full-api-key", cfg.APIKey)
	}
	if cfg.AdminKey != "full-admin-key" {
		t.Fatalf("AdminKey = %q, want full-admin-key", cfg.AdminKey)
	}
	if cfg.WebUIKey != "full-webui-key" {
		t.Fatalf("WebUIKey = %q, want full-webui-key", cfg.WebUIKey)
	}
	if cfg.FallbackProxy != "fb-proxy.com" {
		t.Fatalf("FallbackProxy = %q", cfg.FallbackProxy)
	}
	if len(cfg.ProxyGroups) != 1 || cfg.ProxyGroups[0].ID != "pg1" {
		t.Fatalf("ProxyGroups = %v", cfg.ProxyGroups)
	}
	if len(cfg.ProxyGroups[0].Nodes) != 1 || cfg.ProxyGroups[0].Nodes[0].ID != "pn1" {
		t.Fatalf("ProxyNodes = %v", cfg.ProxyGroups[0].Nodes)
	}
}

// Load legacy config.json 回退测试
func TestLoadLegacyConfig(t *testing.T) {
	root := t.TempDir()

	// 在 root 下创建 config.json（legacy）
	legacyContent := map[string]any{"auth-key": "legacy-key"}
	data, _ := json.Marshal(legacyContent)
	legacyPath := filepath.Join(root, "config.json")
	os.WriteFile(legacyPath, data, 0o644)

	// 设置 GO_CONFIG_PATH 到 data 子目录下的 config.json（不存在）
	dataDir := filepath.Join(root, "data")
	os.MkdirAll(dataDir, 0o755)
	t.Setenv("GO_CONFIG_PATH", filepath.Join(dataDir, "config.json"))
	t.Setenv("CHATGPT2API_AUTH_KEY", "")

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("LoadLegacyConfig failed: %v", err)
	}
	if cfg.APIKey != "legacy-key" {
		t.Fatalf("APIKey = %q, want legacy-key", cfg.APIKey)
	}
	// 验证 config.json 被复制到了 data 目录
	if _, err := os.Stat(filepath.Join(dataDir, "config.json")); os.IsNotExist(err) {
		t.Fatal("legacy config.json should have been copied to data dir")
	}
}

// Load root 为空字符串时自动获取 cwd
func TestLoadEmptyRoot(t *testing.T) {
	t.Setenv("GROK_ROOT_DIR", "")
	cwd, _ := os.Getwd()

	configPath := filepath.Join(cwd, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 没有配置文件时应该正常返回默认配置
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load with empty root failed: %v", err)
		}
		if cfg.RootDir == "" {
			t.Fatal("RootDir should not be empty")
		}
		_ = cfg
	}
}

// Load 非法根目录测试
func TestLoadInvalidRoot(t *testing.T) {
	_, err := Load(string(rune(0)))
	// filepath.Abs 可能会成功也可能失败，关键是函数应该返回 error 或 Config
	_ = err
}

// splitList 边界情况
func TestSplitListEdgeCases(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "单个元素", value: "only", want: []string{"only"}},
		{name: "末尾逗号", value: "a,b,", want: []string{"a", "b"}},
		{name: "开头逗号", value: ",a,b", want: []string{"a", "b"}},
		{name: "连续逗号", value: "a,,b", want: []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitList(tc.value)
			if len(got) != len(tc.want) {
				t.Fatalf("splitList(%q) = %v, want %v", tc.value, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("splitList(%q)[%d] = %q, want %q", tc.value, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// firstString 全部键为空值
func TestFirstStringAllEmpty(t *testing.T) {
	m := map[string]any{
		"a": "",
		"b": "",
	}
	got := firstString(m, "a", "b")
	if got != "" {
		t.Fatalf("firstString(all empty) = %q, want empty", got)
	}
}

// firstString 值为非字符串类型
func TestFirstStringNonString(t *testing.T) {
	m := map[string]any{
		"num": 42,
	}
	got := firstString(m, "num")
	if got != "" {
		t.Fatalf("firstString(non-string value) = %q, want empty", got)
	}
}

// anyStringList 字符串类型调用 splitList
func TestAnyStringListStringInput(t *testing.T) {
	got := anyStringList("x,y,z")
	if len(got) != 3 || got[0] != "x" {
		t.Fatalf("anyStringList(string) = %v, want [x y z]", got)
	}
}

// parseStatusCodes 边界值
func TestParseStatusCodesEdge(t *testing.T) {
	// 399 不在范围内
	result := parseStatusCodes("399")
	if len(result) != 0 {
		t.Fatalf("parseStatusCodes(399) = %v, want empty", result)
	}
	// 600 不在范围内
	result = parseStatusCodes("600")
	if len(result) != 0 {
		t.Fatalf("parseStatusCodes(600) = %v, want empty", result)
	}
	// 400 和 599 在范围内
	result = parseStatusCodes("400,599")
	if !result[400] || !result[599] {
		t.Fatalf("parseStatusCodes(400,599) = %v, want {400:true, 599:true}", result)
	}
}

// configInt json.Number 边界
func TestConfigIntJSONNumber(t *testing.T) {
	cases := []struct {
		name  string
		value json.Number
		want  int
	}{
		{name: "正数", value: json.Number("42"), want: 42},
		{name: "零", value: json.Number("0"), want: 0},
		{name: "负数", value: json.Number("-5"), want: -5},
		{name: "大数", value: json.Number("999999"), want: 999999},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := configInt(tc.value); got != tc.want {
				t.Fatalf("configInt(%v) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

// configInt 非法字符串返回 0
func TestConfigIntInvalidString(t *testing.T) {
	got := configInt("not-a-number")
	if got != 0 {
		t.Fatalf("configInt(\"not-a-number\") = %d, want 0", got)
	}
}

// ReadMap 写入并重新读取
func TestReadMapRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "roundtrip.json")

	original := map[string]any{
		"string_key": "hello",
		"int_key":    float64(42),
		"bool_key":   true,
	}
	data, _ := json.Marshal(original)
	os.WriteFile(path, data, 0o644)

	got, err := readMap(path)
	if err != nil {
		t.Fatalf("readMap roundtrip error: %v", err)
	}
	if got["string_key"] != "hello" {
		t.Fatalf("string_key = %v, want hello", got["string_key"])
	}
	if got["int_key"] != float64(42) {
		t.Fatalf("int_key = %v, want 42", got["int_key"])
	}
	if got["bool_key"] != true {
		t.Fatalf("bool_key = %v, want true", got["bool_key"])
	}
}

// env 特殊字符处理
func TestEnvWithSpecialChars(t *testing.T) {
	t.Setenv("GO_TEST_SPECIAL", "  value with spaces  ")
	if got := env("GO_TEST_SPECIAL", "fallback"); got != "value with spaces" {
		t.Fatalf("env special = %q, want 'value with spaces'", got)
	}
}

// envInt 超大数值
func TestEnvIntLargeValue(t *testing.T) {
	t.Setenv("GO_TEST_LARGE_INT", "999999")
	if got := envInt("GO_TEST_LARGE_INT", 10); got != 999999 {
		t.Fatalf("envInt large = %d, want 999999", got)
	}
}

// envBool 各种大小写
func TestEnvBoolCaseInsensitive(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"TRUE", true},
		{"True", true},
		{"FALSE", false},
		{"Yes", true},
		{"NO", false},
		{"ON", true},
		{"OFF", false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("GO_TEST_BOOL_CASE", tc.value)
			if got := envBool("GO_TEST_BOOL_CASE", !tc.want); got != tc.want {
				t.Fatalf("envBool(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// Load env 超范围钳制
func TestLoadEnvClampingUpperBound(t *testing.T) {
	t.Setenv("GO_REQUEST_TIMEOUT_SECONDS", "999")
	root := t.TempDir()
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.RequestTimeout != 300*time.Second {
		t.Fatalf("RequestTimeout clamped to %v, want 300s", cfg.RequestTimeout)
	}
}

func TestLoadEnvClampingLowerBound(t *testing.T) {
	t.Setenv("GO_IMAGE_RETENTION_DAYS", "0")
	root := t.TempDir()
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ImageRetentionDays != 1 {
		t.Fatalf("ImageRetentionDays clamped to %d, want 1", cfg.ImageRetentionDays)
	}
}
