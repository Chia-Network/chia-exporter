package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "chia-exporter",
	Short: "Prometheus metric exporter for Chia Blockchain",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	var (
		metricsPort          int
		maxmindCountryDBPath string
		maxmindASNDBPath     string
	)

	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.chia-exporter.yaml)")

	rootCmd.PersistentFlags().IntVar(&metricsPort, "metrics-port", 9914, "The port the metrics server binds to")
	rootCmd.PersistentFlags().StringVar(&maxmindCountryDBPath, "maxmind-country-db-path", "", "Path to the maxmind country database file")
	rootCmd.PersistentFlags().StringVar(&maxmindASNDBPath, "maxmind-asn-db-path", "", "Path to the maxmind ASN database file")

	viper.BindPFlag("metrics-port", rootCmd.PersistentFlags().Lookup("metrics-port"))
	viper.BindPFlag("maxmind-country-db-path", rootCmd.PersistentFlags().Lookup("maxmind-country-db-path"))
	viper.BindPFlag("maxmind-asn-db-path", rootCmd.PersistentFlags().Lookup("maxmind-asn-db-path"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".chia-exporter" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".chia-exporter")
	}

	viper.SetEnvPrefix("CHIA_EXPORTER")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
