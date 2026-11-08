package config

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config holds the runtime configuration for the agy-cli-api server.
type Config struct {
	Port                  string
	AgyBin                string
	DataDir               string
	DefaultWorkspace      string
	DefaultPermissionMode string
	CORSOrigins           []string
}

// Load reads configuration from optional .env file and environment variables.
func Load() (*Config, error) {
	loadDotEnv(".env")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	port := getEnv("PORT", "8080")
	agyBin := resolveAgyBin(getEnv("AGY_BIN", ""), homeDir)
	dataDir := expandTilde(getEnv("AGY_DATA_DIR", filepath.Join(homeDir, ".gemini", "antigravity-cli")), homeDir)
	defaultWorkspace := expandTilde(getEnv("AGY_DEFAULT_WORKSPACE", cwd), homeDir)
	defaultPermissionMode := getEnv("AGY_DEFAULT_PERMISSION_MODE", "skip-permissions")
	corsRaw := getEnv("AGY_CORS_ORIGINS", "*")

	var corsOrigins []string
	for _, o := range strings.Split(corsRaw, ",") {
		trimmed := strings.TrimSpace(o)
		if trimmed != "" {
			corsOrigins = append(corsOrigins, trimmed)
		}
	}
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"*"}
	}

	return &Config{
		Port:                  port,
		AgyBin:                agyBin,
		DataDir:               dataDir,
		DefaultWorkspace:      defaultWorkspace,
		DefaultPermissionMode: defaultPermissionMode,
		CORSOrigins:           corsOrigins,
	}, nil
}

func resolveAgyBin(configured, homeDir string) string {
	if configured != "" {
		expanded := expandTilde(configured, homeDir)
		if path, err := exec.LookPath(expanded); err == nil {
			return path
		}
		return expanded
	}
	if path, err := exec.LookPath("agy"); err == nil {
		return path
	}
	localBin := filepath.Join(homeDir, ".local", "bin", "agy")
	if _, err := os.Stat(localBin); err == nil {
		return localBin
	}
	return "agy"
}

func expandTilde(path, homeDir string) string {
	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return fallback
}

func loadDotEnv(filename string) {
	f, err := os.Open(filename)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}

