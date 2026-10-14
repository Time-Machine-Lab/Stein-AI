package execenv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"gopkg.in/yaml.v3"
)

// OMP's current native format is YAML, and agent.db co-locates credentials with
// unrelated state. The native sources of truth are omp.sh/docs/custom-models,
// omp.sh/docs/secrets and packages/ai/src/auth/sqlite-credential-store.ts in
// github.com/can1357/oh-my-pi. Do not copy the SQLite database or discovery roots.
func prepareGraphOMPHome(env *Environment, params PrepareParams) error {
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
	source, configRoot, dataDir, err := graphOMPSource(home, lookup, params.Task.GraphEnvironment)
	if err != nil {
		return err
	}
	target := filepath.Join(env.GraphHome, ".omp", "agent")
	dotenv, err := graphOMPDotenv(home, configRoot, source)
	if err != nil {
		return err
	}
	lookup = func(key string) string {
		if value, explicit := params.Task.GraphEnvironment[key]; explicit {
			return value
		}
		if value := os.Getenv(key); value != "" {
			return value
		}
		return dotenv[key]
	}
	var credentialEnv strings.Builder
	for key := range dotenv {
		if !graphProviderCredentialKey(key) && !regexp.MustCompile(`^[A-Z0-9_]+_(API_KEY|OAUTH_TOKEN|ACCESS_TOKEN|AUTH_TOKEN|BASE_URL)$`).MatchString(key) {
			continue
		}
		value := lookup(key)
		if value == "" {
			continue
		}
		quote := "'"
		if strings.Contains(value, quote) {
			quote = "\""
		}
		if strings.Contains(value, quote) {
			quote = "`"
		}
		if strings.Contains(value, quote) {
			return fmt.Errorf("OMP dotenv credential cannot be safely quoted")
		}
		credentialEnv.WriteString(key + "=" + quote + value + quote + "\n")
	}
	if err := graphNativeWrite(filepath.Join(target, ".env"), []byte(credentialEnv.String())); err != nil {
		return err
	}
	models, exists, err := graphOMPReadYAML(source, "models")
	if err != nil {
		return err
	}
	if !exists {
		models, err = graphNativeReadJSON(filepath.Join(source, "models.json"), false)
		if err != nil {
			return err
		}
	}
	if len(models) > 0 {
		if err := graphOMPSanitizeModels(models, lookup); err != nil {
			return err
		}
		if err := graphOMPWriteYAML(filepath.Join(target, "models.yml"), models); err != nil {
			return err
		}
	}
	config, _, err := graphOMPReadYAML(source, "config")
	if err != nil {
		return err
	}
	broker := map[string]any{}
	if value, ok := config["auth"]; ok {
		auth, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid OMP authentication configuration")
		}
		if value, ok := auth["broker"]; ok {
			original, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid OMP auth broker configuration")
			}
			broker = graphNativePick(original, "url", "token")
		}
	}
	for key, variable := range map[string]string{"url": "OMP_AUTH_BROKER_URL", "token": "OMP_AUTH_BROKER_TOKEN"} {
		if value, explicit := params.Task.GraphEnvironment[variable]; explicit {
			broker[key] = value
		} else if value := lookup(variable); value != "" {
			broker[key] = value
		}
	}
	if broker["url"] != nil && broker["url"] != "" && (broker["token"] == nil || broker["token"] == "") {
		if _, explicit := params.Task.GraphEnvironment["OMP_AUTH_BROKER_TOKEN"]; explicit {
			return fmt.Errorf("Graph OMP auth broker requires a token")
		}
		// Native token fallback belongs to the active config root, not agentDir.
		raw, exists, err := graphNativeReadFile(filepath.Join(configRoot, "auth-broker.token"), false)
		if err != nil {
			return err
		}
		if exists {
			broker["token"] = strings.TrimSpace(string(raw))
		}
		if broker["token"] == nil || broker["token"] == "" {
			return fmt.Errorf("Graph OMP auth broker requires a token")
		}
	}
	for _, key := range []string{"url", "token"} {
		if value, ok := broker[key]; ok {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("invalid OMP auth broker configuration")
			}
		}
	}
	config = map[string]any{"mcp": map[string]any{"enableProjectConfig": false}, "browser": map[string]any{"enabled": false}}
	if len(broker) > 0 {
		config["auth"] = map[string]any{"broker": broker}
	}
	if err := graphOMPWriteYAML(filepath.Join(target, "config.yml"), config); err != nil {
		return err
	}
	var mcp map[string]any
	if len(params.McpConfig) > 0 && string(params.McpConfig) != "null" {
		if err := json.Unmarshal(params.McpConfig, &mcp); err != nil {
			return fmt.Errorf("invalid frozen OMP MCP configuration")
		}
	}
	if _, ok := mcp["mcpServers"]; !ok {
		mcp = map[string]any{"mcpServers": mcp}
	}
	if mcp["mcpServers"] == nil {
		mcp["mcpServers"] = map[string]any{}
	}
	if err := writeGraphJSON(filepath.Join(target, "mcp.json"), mcp); err != nil {
		return err
	}
	// Native broker mode replaces local credential storage, without fallback.
	if url, _ := broker["url"].(string); url != "" {
		return nil
	}
	return graphOMPCopySQLiteCredentials(filepath.Join(dataDir, "agent.db"), filepath.Join(target, "agent.db"))
}

func graphOMPSource(home string, lookup func(string) string, explicit map[string]string) (source, root, data string, err error) {
	name := lookup("PI_CONFIG_DIR")
	if name == "" {
		name = ".omp"
	}
	// OMP documents a directory name below HOME, not an arbitrary absolute root.
	if filepath.IsAbs(name) || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", "", "", fmt.Errorf("Graph OMP PI_CONFIG_DIR must name one directory below home")
	}
	root = filepath.Join(home, name)
	profile, exists := explicit["OMP_PROFILE"]
	if !exists {
		profile, exists = os.LookupEnv("OMP_PROFILE")
	}
	if !exists {
		profile = lookup("PI_PROFILE")
	}
	profile = strings.TrimSpace(profile)
	if profile == "default" {
		profile = ""
	}
	if profile != "" {
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`).MatchString(profile) || strings.HasSuffix(profile, ".") || regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])(\..*)?$`).MatchString(profile) {
			return "", "", "", fmt.Errorf("invalid OMP source profile")
		}
		root = filepath.Join(root, "profiles", profile)
	}
	source = filepath.Join(root, "agent")
	defaultSource := source
	if profile == "" && lookup("PI_CODING_AGENT_DIR") != "" {
		source, err = graphNativeSourceDir(lookup, "PI_CODING_AGENT_DIR", source)
		if err != nil {
			return "", "", "", err
		}
	}
	data = source
	if runtime.GOOS != "windows" && source == defaultSource && lookup("XDG_DATA_HOME") != "" {
		candidate := filepath.Join(lookup("XDG_DATA_HOME"), "omp")
		if profile != "" {
			candidate = filepath.Join(candidate, "profiles", profile)
		}
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			data = candidate
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return "", "", "", fmt.Errorf("cannot inspect OMP XDG credential directory")
		}
	}
	return source, root, data, nil
}

func graphOMPReadYAML(dir, stem string) (map[string]any, bool, error) {
	for _, suffix := range []string{".yml", ".yaml"} {
		raw, exists, err := graphNativeReadFile(filepath.Join(dir, stem+suffix), false)
		if err != nil {
			return nil, false, err
		}
		if !exists {
			continue
		}
		var result map[string]any
		if err := yaml.Unmarshal(raw, &result); err != nil || result == nil {
			return nil, false, fmt.Errorf("invalid OMP %s YAML", stem)
		}
		return result, true, nil
	}
	return map[string]any{}, false, nil
}

// OMP's parser keeps values literal, including quoted newlines; it does not
// execute commands or expand shell syntax. Only credential names are emitted,
// while custom catalog references can resolve any explicitly named value.
// Precedence: agent .env, config-root .env, home .env (the launch directory is
// deliberately excluded because that is the new isolated attempt checkout).
func graphOMPDotenv(home, root, source string) (map[string]string, error) {
	result := map[string]string{}
	pattern := regexp.MustCompile("(?m)^[\\t ]*(?:export[\\t ]+)?([A-Za-z_][A-Za-z0-9_]*)[\\t ]*=[\\t ]*(?:'([^']*)'|\"([^\"]*)\"|`([^`]*)`|([^\\r\\n#]*))")
	for _, dir := range []string{home, root, source} {
		raw, exists, err := graphNativeReadFile(filepath.Join(dir, ".env"), false)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		values := map[string]string{}
		for _, match := range pattern.FindAllStringSubmatch(string(raw), -1) {
			value := ""
			for i := 2; i < len(match); i++ {
				if match[i] != "" {
					value = match[i]
					if i == 5 {
						value = strings.TrimSpace(value)
					}
					break
				}
			}
			values[match[1]] = value
		}
		// The native dotenv compatibility alias is per file, with OMP_ winning.
		for key, value := range values {
			if strings.HasPrefix(key, "OMP_") {
				values["PI_"+strings.TrimPrefix(key, "OMP_")] = value
			}
		}
		for key, value := range values {
			if value != "" {
				result[key] = value
			}
		}
	}
	return result, nil
}

func graphOMPWriteYAML(path string, value any) error {
	raw, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("invalid OMP native configuration")
	}
	if len(raw) > graphNativeConfigLimit {
		return fmt.Errorf("resolved OMP native configuration exceeds 4 MiB")
	}
	return graphNativeWrite(path, raw)
}

func graphOMPSanitizeModels(root map[string]any, lookup func(string) string) error {
	for key := range root {
		if key != "providers" {
			return fmt.Errorf("unsupported OMP model catalog root field")
		}
	}
	providers, ok := root["providers"].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid OMP provider catalog")
	}
	allowed := strings.Fields("baseUrl api apiKey auth headers authHeader models discovery modelOverrides compat disableStrictTools guardrailIdentifier guardrailVersion guardrailTrace transport")
	for _, value := range providers {
		entry, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid OMP provider entry")
		}
		for key := range entry {
			found := false
			for _, name := range allowed {
				if key == name {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("unsupported OMP provider configuration field %s", key)
			}
		}
		if err := graphOMPResolveSecrets(entry, lookup); err != nil {
			return err
		}
	}
	return nil
}

// apiKey and headers use native env-name-or-literal syntax; a leading ! is an
// executable secret helper. Reject it rather than executing mutable code.
func graphOMPResolveSecrets(value any, lookup func(string) string) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "compactionModel" || key == "contextPromotionTarget" || key == "remoteCompaction" {
				return fmt.Errorf("Graph OMP cannot freeze native model switching configuration")
			}
			if key == "apiKey" {
				text, ok := child.(string)
				if !ok {
					return fmt.Errorf("invalid OMP API credential")
				}
				resolved, err := graphOMPResolveSecret(text, lookup)
				if err != nil {
					return err
				}
				typed[key] = resolved
			} else if key == "headers" {
				headers, ok := child.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid OMP provider headers")
				}
				for header, value := range headers {
					text, ok := value.(string)
					if !ok {
						return fmt.Errorf("invalid OMP provider header")
					}
					resolved, err := graphOMPResolveSecret(text, lookup)
					if err != nil {
						return err
					}
					headers[header] = resolved
				}
			} else if err := graphOMPResolveSecrets(child, lookup); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := graphOMPResolveSecrets(child, lookup); err != nil {
				return err
			}
		}
	}
	return nil
}

func graphOMPResolveSecret(value string, lookup func(string) string) (string, error) {
	if strings.HasPrefix(strings.TrimSpace(value), "!") {
		return "", fmt.Errorf("Graph OMP cannot freeze executable credential helpers; configure a token environment variable")
	}
	if resolved := lookup(value); resolved != "" {
		if strings.HasPrefix(strings.TrimSpace(resolved), "!") {
			return "", fmt.Errorf("OMP resolved credential cannot begin with executable-helper marker")
		}
		return resolved, nil
	}
	return value, nil
}

func graphOMPCopySQLiteCredentials(source, target string) error {
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("OMP credential source must be a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	staged, err := os.CreateTemp(filepath.Dir(target), ".auth-*.db")
	if err != nil {
		return err
	}
	path := staged.Name()
	if err := staged.Close(); err != nil {
		return err
	}
	defer os.Remove(path)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.Command("node", "--no-warnings", "-e", graphOMPSQLiteProjection, source, path)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !strings.EqualFold(key, "NODE_OPTIONS") && !strings.EqualFold(key, "NODE_PATH") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	// Suppress both streams: native database errors may contain credential JSON.
	if err := processtree.Run(ctx, cmd, 2*time.Second); err != nil {
		return fmt.Errorf("cannot project OMP credentials: supported auth schema and Node.js 22 with node:sqlite are required: %w", err)
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("private OMP credential database already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(path, target)
}

// Source connection is read-only and sees committed WAL rows. Only versions
// with the documented v3 credential columns (schema 3..8) are accepted. Output
// is a fresh v3 auth table; OMP installs its empty caches and newer schema on boot.
const graphOMPSQLiteProjection = `const {DatabaseSync}=require('node:sqlite');
const [source,target]=process.argv.slice(1);const src=new DatabaseSync(source,{readOnly:true});let dst;
try {
src.exec('PRAGMA trusted_schema=OFF; PRAGMA query_only=ON; PRAGMA busy_timeout=1000');
const table=src.prepare("SELECT type FROM sqlite_master WHERE name='auth_credentials'").get();
if(!table||table.type!=='table')throw Error('Unsupported credential table');
const version=src.prepare('SELECT version FROM auth_schema_version WHERE id=1').get()?.version;
if(!Number.isInteger(version)||version<3||version>8)throw Error('Unsupported authentication schema');
const columns=src.prepare('PRAGMA table_info(auth_credentials)').all().map(x=>x.name).sort();
if(JSON.stringify(columns)!==JSON.stringify(['id','provider','credential_type','data','disabled_cause','identity_key','created_at','updated_at'].sort()))throw Error('Unsupported credential columns');
const bound=src.prepare("SELECT COUNT(*) n, COALESCE(SUM(length(CAST(data AS BLOB))+length(provider)+COALESCE(length(identity_key),0)),0) bytes FROM auth_credentials WHERE disabled_cause IS NULL").get();
if(bound.n>4096||bound.bytes>4*1024*1024)throw Error('Credential budget exceeded');
const rows=src.prepare('SELECT id,provider,credential_type,data,identity_key,created_at,updated_at FROM auth_credentials WHERE disabled_cause IS NULL ORDER BY id').all();
for(const row of rows){if(typeof row.provider!=='string'||!row.provider||typeof row.data!=='string')throw Error('Invalid credential');const value=JSON.parse(row.data);if(!value||typeof value!=='object'||Array.isArray(value))throw Error('Invalid credential');if(row.credential_type==='api_key'){if(typeof value.key!=='string'||!value.key)throw Error('Invalid API key');}else if(row.credential_type==='oauth'){if(typeof value.access!=='string'||typeof value.refresh!=='string'||!Number.isFinite(value.expires))throw Error('Invalid OAuth credential');}else throw Error('Unsupported credential type');}
dst=new DatabaseSync(target);dst.exec("CREATE TABLE auth_schema_version(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);INSERT INTO auth_schema_version VALUES(1,3);CREATE TABLE auth_credentials(id INTEGER PRIMARY KEY AUTOINCREMENT,provider TEXT NOT NULL,credential_type TEXT NOT NULL,data TEXT NOT NULL,disabled_cause TEXT DEFAULT NULL,identity_key TEXT DEFAULT NULL,created_at INTEGER NOT NULL DEFAULT(CAST(strftime('%s','now') AS INTEGER)),updated_at INTEGER NOT NULL DEFAULT(CAST(strftime('%s','now') AS INTEGER)));");
dst.exec('BEGIN');const insert=dst.prepare('INSERT INTO auth_credentials(id,provider,credential_type,data,identity_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?)');for(const row of rows)insert.run(row.id,row.provider,row.credential_type,row.data,row.identity_key,row.created_at??0,row.updated_at??0);dst.exec('COMMIT');
} finally {if(dst)dst.close();src.close();}`
