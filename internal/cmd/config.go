package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	adpmconfig "github.com/afterdarksys/adpm/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

var (
	configPath        string
	autoConfigPath    string
	effectiveConfig   map[string]interface{}
	effectiveAuto     map[string]interface{}
	configSources     []string
	autoConfigSources []string
)

func loadConfiguration(cmd *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	effectiveConfig, configSources, err = adpmconfig.Load(adpmconfig.CandidatePaths(
		"adpm.cfg", home, os.Getenv("ADPM_CONFIG"), configPath))
	if err != nil {
		return err
	}
	effectiveAuto, autoConfigSources, err = adpmconfig.Load(adpmconfig.CandidatePaths(
		"adpm_auto.cfg", home, os.Getenv("ADPM_AUTO_CONFIG"), autoConfigPath))
	if err != nil {
		return err
	}

	setEnvironmentDefault("ADPM_DB", expandConfiguredPath(configString(effectiveConfig, "database"), home))
	setEnvironmentDefault("ADPM_PREFIX", expandConfiguredPath(configString(effectiveConfig, "prefix"), home))
	setEnvironmentDefault("ADPM_TRUSTED_KEYS", expandConfiguredPath(configString(effectiveConfig, "trusted_keys"), home))
	setEnvironmentDefault("ADPM_AUTO_ENABLED", configString(effectiveAuto, "enabled"))
	if err := applyCommandDefaults(cmd, effectiveConfig); err != nil {
		return fmt.Errorf("adpm.cfg: %w", err)
	}
	if strings.EqualFold(os.Getenv("ADPM_AUTO_ENABLED"), "true") {
		setEnvironmentDefault("ADPM_NON_INTERACTIVE", configString(effectiveAuto, "non_interactive"))
		setEnvironmentDefault("ADPM_ASSUME_YES", configString(effectiveAuto, "assume_yes"))
		answers, err := json.Marshal(effectiveAuto["answers"])
		if err != nil {
			return fmt.Errorf("adpm_auto.cfg answers: %w", err)
		}
		if string(answers) != "null" {
			setEnvironmentDefault("ADPM_AUTO_ANSWERS", string(answers))
		}
		if err := applyCommandDefaults(cmd, effectiveAuto); err != nil {
			return fmt.Errorf("adpm_auto.cfg: %w", err)
		}
	}
	return nil
}

func setEnvironmentDefault(name, value string) {
	if value != "" && os.Getenv(name) == "" {
		_ = os.Setenv(name, value)
	}
}

func configString(config map[string]interface{}, key string) string {
	if value, ok := config[key]; ok {
		return fmt.Sprint(value)
	}
	return ""
}

func expandConfiguredPath(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return home + string(os.PathSeparator) + strings.TrimPrefix(path, "~/")
	}
	return path
}

func applyCommandDefaults(cmd *cobra.Command, config map[string]interface{}) error {
	defaults, _ := config["defaults"].(map[string]interface{})
	values, _ := defaults[cmd.Name()].(map[string]interface{})
	for name, value := range values {
		flag := cmd.Flags().Lookup(strings.ReplaceAll(name, "_", "-"))
		if flag == nil {
			return fmt.Errorf("unknown default %s.%s", cmd.Name(), name)
		}
		if flag.Changed {
			continue
		}
		if err := setFlagValue(flag, value); err != nil {
			return fmt.Errorf("invalid default for %s.%s: %w", cmd.Name(), name, err)
		}
	}
	return nil
}

func setFlagValue(flag *pflag.Flag, value interface{}) error {
	if values, ok := value.([]interface{}); ok {
		if flag.Value.Type() == "stringArray" {
			for _, item := range values {
				if err := flag.Value.Set(fmt.Sprint(item)); err != nil {
					return err
				}
			}
			return nil
		}
		parts := make([]string, len(values))
		for index, item := range values {
			parts[index] = fmt.Sprint(item)
		}
		return flag.Value.Set(strings.Join(parts, ","))
	}
	return flag.Value.Set(fmt.Sprint(value))
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show effective CLI and automation configuration",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		output := map[string]interface{}{
			"cli": effectiveConfig, "automation": effectiveAuto,
			"sources": map[string]interface{}{"cli": configSources, "automation": autoConfigSources},
		}
		encoded, err := yaml.Marshal(output)
		if err != nil {
			return err
		}
		fmt.Print(string(encoded))
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Explicit adpm.cfg path (also ADPM_CONFIG)")
	rootCmd.PersistentFlags().StringVar(&autoConfigPath, "auto-config", "", "Explicit adpm_auto.cfg path (also ADPM_AUTO_CONFIG)")
	rootCmd.PersistentPreRunE = loadConfiguration
	rootCmd.AddCommand(configCmd)
}
