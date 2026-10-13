package execenv

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/tidwall/pretty"
	"gopkg.in/yaml.v3"
)

const graphNativeConfigLimit = 4 << 20

// prepareGraphExtraProvider imports authentication/transport into an attempt's
// private native configuration roots. Model catalogs describe API routing;
// selected models, instructions, extensions, MCP and session state are not copied.
// The daemon must redirect the corresponding native environment selectors after
// preparation. Values supplied by the immutable run take precedence over ambient
// selectors, including explicitly empty values.
func prepareGraphExtraProvider(env *Environment, params PrepareParams) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	lookup := func(key string) string {
		if value, ok := params.Task.GraphEnvironment[key]; ok {
			return value
		}
		return os.Getenv(key)
	}
	switch params.Provider {
	case "codebuddy":
		// https://www.codebuddy.ai/docs/cli/settings and /cli/env-vars.
		source, err := graphNativeSourceDir(lookup, "CODEBUDDY_CONFIG_DIR", filepath.Join(home, ".codebuddy"))
		if err != nil {
			return err
		}
		original, err := graphNativeReadJSON(filepath.Join(source, "settings.json"), false)
		if err != nil {
			return err
		}
		if helper, ok := original["apiKeyHelper"]; ok && helper != nil && helper != "" {
			return fmt.Errorf("Graph CodeBuddy cannot freeze apiKeyHelper; configure CODEBUDDY_API_KEY or CODEBUDDY_AUTH_TOKEN instead")
		}
		selected := map[string]any{}
		if value, ok := original["env"]; ok {
			values, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("CodeBuddy settings.env must be an object")
			}
			for key, value := range values {
				if graphCodeBuddyTransportKey(key) {
					if _, ok := value.(string); !ok {
						return fmt.Errorf("CodeBuddy transport environment values must be strings")
					}
					selected[key] = value
				}
			}
		}
		for key, value := range params.Task.GraphEnvironment {
			if graphCodeBuddyTransportKey(key) {
				selected[key] = value
			}
		}
		return writeGraphJSON(filepath.Join(env.GraphHome, ".codebuddy", "settings.json"), map[string]any{"env": selected})
	case "copilot":
		// https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-config-dir-reference
		// config.json mixes authentication, plugins and permissions. Its internal
		// credential schema is not public; automation uses the documented token env.
		source, err := graphNativeSourceDir(lookup, "COPILOT_HOME", filepath.Join(home, ".copilot"))
		if err != nil {
			return err
		}
		registry := filepath.Join(source, "providers.json")
		required := false
		if value := strings.TrimSpace(lookup("COPILOT_PROVIDERS_CONFIG")); value != "" {
			registry, err = graphNativeAbsolutePath(value)
			if err != nil {
				return err
			}
			required = true
		}
		providers, err := graphNativeReadJSON(registry, required)
		if err != nil {
			return err
		}
		if err := writeGraphJSON(filepath.Join(env.GraphHome, ".copilot", "providers.json"), graphNativePick(providers, "providers", "models")); err != nil {
			return err
		}
		settings, err := graphNativeReadJSON(filepath.Join(source, "settings.json"), false)
		if err != nil {
			return err
		}
		return writeGraphJSON(filepath.Join(env.GraphHome, ".copilot", "settings.json"), graphNativePick(settings, "proxyUrl", "proxyKerberosServicePrincipal"))
	case "cursor":
		// https://cursor.com/docs/cli/reference/configuration and /authentication.
		// Browser login storage is opaque; copying IDE state also imports sessions.
		if strings.TrimSpace(lookup("CURSOR_API_KEY")) == "" {
			return fmt.Errorf("Graph Cursor requires CURSOR_API_KEY in the runtime environment; browser credentials cannot be safely isolated")
		}
		fallback := filepath.Join(home, ".cursor")
		if runtime.GOOS != "windows" && runtime.GOOS != "darwin" && lookup("XDG_CONFIG_HOME") != "" {
			fallback = filepath.Join(lookup("XDG_CONFIG_HOME"), "cursor")
		}
		source, err := graphNativeSourceDir(lookup, "CURSOR_CONFIG_DIR", fallback)
		if err != nil {
			return err
		}
		original, err := graphNativeReadJSON(filepath.Join(source, "cli-config.json"), false)
		if err != nil {
			return err
		}
		selected := map[string]any{"version": 1, "editor": map[string]any{"vimMode": false}, "permissions": map[string]any{"allow": []string{}, "deny": []string{}}}
		if network, ok := original["network"]; ok {
			values, ok := network.(map[string]any)
			if !ok {
				return fmt.Errorf("Cursor network configuration must be an object")
			}
			selected["network"] = graphNativePick(values, "useHttp1ForAgent")
		}
		return writeGraphJSON(filepath.Join(env.GraphHome, ".cursor", "cli-config.json"), selected)
	case "qoder", "qoder-cn", "qoderclicn":
		// https://docs.qoder.com/cli/settings-reference and /cli/authentication.
		// modelConfigs contains behavior overrides, not a portable BYOK store:
		// https://docs.qoder.com/cli/custom-models explicitly documents this boundary.
		if strings.TrimSpace(lookup("QODER_PERSONAL_ACCESS_TOKEN")) == "" {
			return fmt.Errorf("Graph %s requires QODER_PERSONAL_ACCESS_TOKEN; browser and BYOK account stores cannot be safely isolated", params.Provider)
		}
		folder := "." + params.Provider
		if params.Provider == "qoderclicn" {
			folder = ".qoder-cn"
		}
		source, err := graphNativeSourceDir(lookup, "QODER_CONFIG_DIR", filepath.Join(home, folder))
		if err != nil {
			return err
		}
		original, err := graphNativeReadJSON(filepath.Join(source, "settings.json"), false)
		if err != nil {
			return err
		}
		return writeGraphJSON(filepath.Join(env.GraphHome, folder, "settings.json"), graphNativePick(original, "vpcInstanceName"))
	case "kiro":
		// https://kiro.dev/docs/getting-started/authentication/ documents API-key
		// automation. Its browser credential/session databases are not copied.
		if strings.TrimSpace(lookup("KIRO_API_KEY")) == "" {
			return fmt.Errorf("Graph Kiro requires KIRO_API_KEY; browser credential databases cannot be safely isolated")
		}
		return nil
	case "opencode", "deveco":
		return prepareGraphOpenCodeTransport(env, params, home, lookup)
	case "traecli":
		return prepareGraphTraeTransport(env, home, lookup)
	case "dsh":
		// The first-party credential store is expressly credential-only:
		// https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/credentials/credentials-local/README.md
		source, err := graphNativeSourceDir(lookup, "DSH_HOME", filepath.Join(home, ".dsh"))
		if err != nil {
			return err
		}
		raw, exists, err := graphNativeReadFile(filepath.Join(source, ".credentials.yaml"), false)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		var document map[string]any
		if err := yaml.Unmarshal(raw, &document); err != nil {
			return fmt.Errorf("invalid DSH credential YAML")
		}
		// Let the native store enforce its versioned value schema without copying
		// unrelated files or surfacing its secret-bearing parser diagnostics here.
		if version, ok := document["version"]; ok {
			if version != 1 {
				return fmt.Errorf("unsupported DSH credential document version")
			}
			for key := range document {
				if key != "version" && key != "refs" && key != "records" {
					return fmt.Errorf("unsupported field in DSH credential document")
				}
			}
		} else {
			for _, value := range document {
				if text, ok := value.(string); !ok || text == "" {
					return fmt.Errorf("invalid legacy DSH credential document")
				}
			}
		}
		return graphNativeWrite(filepath.Join(env.GraphHome, ".dsh", ".credentials.yaml"), raw)
	}
	return nil
}

func graphCodeBuddyTransportKey(key string) bool {
	switch key {
	case "CODEBUDDY_API_KEY", "CODEBUDDY_AUTH_TOKEN", "CODEBUDDY_CUSTOM_HEADERS", "CODEBUDDY_BASE_URL", "CODEBUDDY_INTERNET_ENVIRONMENT", "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy":
		return true
	}
	return false
}

func graphNativePick(original map[string]any, keys ...string) map[string]any {
	result := map[string]any{}
	for _, key := range keys {
		if value, ok := original[key]; ok {
			result[key] = value
		}
	}
	return result
}

func graphNativeAbsolutePath(value string) (string, error) {
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, value[2:])
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("native Graph configuration source must be an absolute path")
	}
	return filepath.Clean(value), nil
}

func graphNativeSourceDir(lookup func(string) string, key, fallback string) (string, error) {
	value := strings.TrimSpace(lookup(key))
	if value == "" {
		return fallback, nil
	}
	source, err := graphNativeAbsolutePath(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("configured %s directory is unavailable", key)
	}
	return source, nil
}

func graphNativeReadFile(path string, required bool) ([]byte, bool, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) && !required {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read native configuration %s: %w", filepath.Base(path), err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("native configuration must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, graphNativeConfigLimit+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > graphNativeConfigLimit {
		return nil, false, fmt.Errorf("native configuration exceeds 4 MiB")
	}
	return data, true, nil
}

func graphNativeReadJSON(path string, required bool) (map[string]any, error) {
	raw, exists, err := graphNativeReadFile(path, required)
	if err != nil {
		return nil, err
	}
	if !exists {
		return map[string]any{}, nil
	}
	var result map[string]any
	// Native providers document JSONC. Spec handles comments and trailing commas
	// while preserving strings; JSON decoding still rejects malformed input.
	if err := json.Unmarshal(pretty.Spec(raw), &result); err != nil || result == nil {
		return nil, fmt.Errorf("invalid native JSON configuration in %s", filepath.Base(path))
	}
	return result, nil
}

func graphNativeWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func prepareGraphOpenCodeTransport(env *Environment, params PrepareParams, home string, lookup func(string) string) error {
	provider := params.Provider
	prefix := strings.ToUpper(provider)
	// Managed files outrank inline settings and cannot be neutralized by the
	// private HOME. Do not bypass enterprise policy or pretend it was frozen.
	// https://opencode.ai/docs/config/#managed-settings
	if err := graphRejectManagedNativeConfig(provider); err != nil {
		return err
	}
	configRoot := lookup("XDG_CONFIG_HOME")
	if configRoot == "" {
		configRoot = filepath.Join(home, ".config")
	}
	dataRoot := lookup("XDG_DATA_HOME")
	if dataRoot == "" {
		dataRoot = filepath.Join(home, ".local", "share")
	}
	configDir := filepath.Join(configRoot, provider)
	paths := []struct {
		path     string
		required bool
	}{{filepath.Join(configDir, "config.json"), false}, {filepath.Join(configDir, provider+".json"), false}, {filepath.Join(configDir, provider+".jsonc"), false}}
	if value := strings.TrimSpace(lookup(prefix + "_CONFIG")); value != "" {
		path, err := graphNativeAbsolutePath(value)
		if err != nil {
			return err
		}
		paths = append(paths, struct {
			path     string
			required bool
		}{path, true})
	}
	if value := strings.TrimSpace(lookup(prefix + "_CONFIG_DIR")); value != "" {
		dir, err := graphNativeSourceDir(lookup, prefix+"_CONFIG_DIR", "")
		if err != nil {
			return err
		}
		for _, name := range []string{provider + ".json", provider + ".jsonc"} {
			paths = append(paths, struct {
				path     string
				required bool
			}{filepath.Join(dir, name), false})
		}
	}
	selected := map[string]any{}
	for _, source := range paths {
		original, err := graphNativeReadJSON(source.path, source.required)
		if err != nil {
			return err
		}
		if err := graphMergeNativeProviderConfig(selected, original, filepath.Dir(source.path), lookup); err != nil {
			return err
		}
	}
	if raw := lookup(prefix + "_CONFIG_CONTENT"); strings.TrimSpace(raw) != "" {
		if len(raw) > graphNativeConfigLimit {
			return fmt.Errorf("native inline configuration exceeds 4 MiB")
		}
		var original map[string]any
		if err := json.Unmarshal(pretty.Spec([]byte(raw)), &original); err != nil || original == nil {
			return fmt.Errorf("invalid %s_CONFIG_CONTENT JSON", prefix)
		}
		if err := graphMergeNativeProviderConfig(selected, original, configDir, lookup); err != nil {
			return err
		}
	}
	if err := writeGraphJSON(filepath.Join(env.GraphHome, "config", provider, provider+".json"), selected); err != nil {
		return err
	}
	auth, err := graphNativeReadJSON(filepath.Join(dataRoot, provider, "auth.json"), false)
	if err != nil {
		return err
	}
	for _, raw := range auth {
		entry, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid %s credential entry", provider)
		}
		kind, _ := entry["type"].(string)
		if kind == "wellknown" {
			return fmt.Errorf("Graph %s cannot freeze well-known remote organization configuration; use API or OAuth provider credentials", provider)
		}
		if kind != "api" && kind != "oauth" {
			return fmt.Errorf("unsupported %s credential type", provider)
		}
	}
	return writeGraphJSON(filepath.Join(env.GraphHome, "data", provider, "auth.json"), auth)
}

func graphMergeNativeProviderConfig(target, original map[string]any, sourceDir string, lookup func(string) string) error {
	raw, exists := original["provider"]
	if !exists {
		return nil
	}
	providers, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("native provider configuration must be an object")
	}
	sanitized := map[string]any{}
	bytesRead := new(int)
	for id, raw := range providers {
		entry, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("native provider entry must be an object")
		}
		selected := graphNativePick(entry, "api", "name", "env", "id", "npm", "options", "models")
		// Provider schemas expose transport plus a model catalog. Reject references
		// that would turn the copied catalog into an executable extension path.
		if npm, ok := selected["npm"].(string); ok && (strings.HasPrefix(npm, ".") || strings.Contains(npm, ":") || filepath.IsAbs(npm)) {
			return fmt.Errorf("Graph native provider cannot relocate a local SDK module; use a package provider")
		}
		resolved, err := graphResolveNativeReferences(selected, sourceDir, lookup, 0, bytesRead)
		if err != nil {
			return err
		}
		sanitized[id] = resolved
	}
	graphNativeMerge(target, map[string]any{"provider": sanitized})
	return nil
}

func graphNativeMerge(target, source map[string]any) {
	for key, value := range source {
		if child, ok := value.(map[string]any); ok {
			if prior, ok := target[key].(map[string]any); ok {
				graphNativeMerge(prior, child)
				continue
			}
		}
		target[key] = value
	}
}

func graphResolveNativeReferences(value any, dir string, lookup func(string) string, depth int, bytesRead *int) (any, error) {
	if depth > 32 {
		return nil, fmt.Errorf("native provider configuration nesting exceeds limit")
	}
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range typed {
			resolved, err := graphResolveNativeReferences(child, dir, lookup, depth+1, bytesRead)
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			resolved, err := graphResolveNativeReferences(child, dir, lookup, depth+1, bytesRead)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	case string:
		var out strings.Builder
		rest := typed
		for {
			start := strings.Index(rest, "{")
			if start < 0 {
				out.WriteString(rest)
				break
			}
			out.WriteString(rest[:start])
			rest = rest[start:]
			end := strings.Index(rest, "}")
			if end < 0 {
				out.WriteString(rest)
				break
			}
			reference := rest[1:end]
			kind, key, ok := strings.Cut(reference, ":")
			if !ok || (kind != "env" && kind != "file") {
				out.WriteString(rest[:end+1])
				rest = rest[end+1:]
				continue
			}
			var content string
			if kind == "env" {
				content = lookup(key)
				if content == "" {
					return nil, fmt.Errorf("native provider environment reference is unset")
				}
			} else {
				path := key
				if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
					var err error
					path, err = graphNativeAbsolutePath(path)
					if err != nil {
						return nil, err
					}
				} else if !filepath.IsAbs(path) {
					path = filepath.Join(dir, path)
				}
				raw, _, err := graphNativeReadFile(path, true)
				if err != nil {
					return nil, err
				}
				content = strings.TrimSpace(string(raw))
			}
			*bytesRead += len(content)
			if *bytesRead > graphNativeConfigLimit {
				return nil, fmt.Errorf("resolved native provider configuration exceeds 4 MiB")
			}
			out.WriteString(content)
			rest = rest[end+1:]
		}
		return out.String(), nil
	}
	return value, nil
}

func graphRejectManagedNativeConfig(provider string) error {
	var roots []string
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("ProgramData")
		if root == "" {
			root = `C:\ProgramData`
		}
		roots = []string{filepath.Join(root, provider)}
	case "darwin":
		roots = []string{filepath.Join("/Library/Application Support", provider)}
	default:
		roots = []string{filepath.Join("/etc", provider)}
	}
	for _, root := range roots {
		for _, name := range []string{provider + ".json", provider + ".jsonc"} {
			info, err := os.Stat(filepath.Join(root, name))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("cannot inspect managed %s configuration", provider)
			}
			if info != nil {
				return fmt.Errorf("Graph %s cannot freeze higher-priority system managed configuration", provider)
			}
		}
	}
	if runtime.GOOS == "darwin" && provider == "opencode" {
		paths := []string{"/Library/Managed Preferences/ai.opencode.managed.plist"}
		current, err := user.Current()
		if err != nil {
			return fmt.Errorf("cannot inspect OpenCode user managed preferences")
		}
		paths = append(paths, filepath.Join("/Library/Managed Preferences", filepath.Base(current.Username), "ai.opencode.managed.plist"))
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("Graph OpenCode cannot freeze macOS managed preferences")
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("cannot inspect OpenCode managed preferences")
			}
		}
	}
	return nil
}

func prepareGraphTraeTransport(env *Environment, home string, lookup func(string) string) error {
	// First-party legacy ACP config and current CLI 2 login-domain config:
	// https://docs.trae.cn/cli_global-settings /cli_model /cli_config-file
	var source, target string
	switch runtime.GOOS {
	case "windows":
		source = filepath.Join(home, "AppData", "Roaming", "trae_cli", "trae_cli.yaml")
		target = filepath.Join(env.GraphHome, "AppData", "Roaming", "trae_cli", "trae_cli.yaml")
	case "darwin":
		source = filepath.Join(home, "Library", "Application Support", "trae_cli", "trae_cli.yaml")
		target = filepath.Join(env.GraphHome, "Library", "Application Support", "trae_cli", "trae_cli.yaml")
	default:
		root := lookup("XDG_CONFIG_HOME")
		if root == "" {
			root = filepath.Join(home, ".config")
		}
		source = filepath.Join(root, "trae_cli", "trae_cli.yaml")
		target = filepath.Join(env.GraphHome, "config", "trae_cli", "trae_cli.yaml")
	}
	raw, exists, err := graphNativeReadFile(source, false)
	if err != nil {
		return err
	}
	if exists {
		var original map[string]any
		if err := yaml.Unmarshal(raw, &original); err != nil {
			return fmt.Errorf("invalid TRAE native YAML configuration")
		}
		selected := graphNativePick(original, "models")
		encoded, err := yaml.Marshal(selected)
		if err != nil {
			return fmt.Errorf("invalid TRAE model catalog")
		}
		if err := graphNativeWrite(target, encoded); err != nil {
			return err
		}
	}
	raw, exists, err = graphNativeReadFile(filepath.Join(home, ".trae", "traecli.toml"), false)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	var original map[string]any
	if err := toml.Unmarshal(raw, &original); err != nil {
		return fmt.Errorf("invalid TRAE native TOML configuration")
	}
	encoded, err := toml.Marshal(graphNativePick(original, "trae_login_base_url"))
	if err != nil {
		return err
	}
	return graphNativeWrite(filepath.Join(env.GraphHome, ".trae", "traecli.toml"), encoded)
}
