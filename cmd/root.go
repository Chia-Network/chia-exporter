package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string
var (
	gitVersion string
	buildTime  string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "chia-exporter",
	Short:   "Prometheus metric exporter for Chia Blockchain",
	Version: versionString(true),
}

func versionString(includeTime bool) string {
	if includeTime {
		return fmt.Sprintf("%s (%s)", gitVersion, buildTime)
	}

	return gitVersion
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
		hostname                          string
		metricsPort                       int
		maxmindCountryDBPath              string
		maxmindASNDBPath                  string
		logLevel                          string
		requestTimeout                    time.Duration
		disableCentralHarvesterCollection bool
		logBlockTimes                     bool

		mysqlHost      string
		mysqlPort      uint16
		mysqlUser      string
		mysqlPass      string
		mysqlDBName    string
		mysqlBatchSize uint32
	)

	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.chia-exporter.yaml)")

	rootCmd.PersistentFlags().StringVar(&hostname, "hostname", "localhost", "The hostname to connect to")
	rootCmd.PersistentFlags().IntVar(&metricsPort, "metrics-port", 9914, "The port the metrics server binds to")
	rootCmd.PersistentFlags().StringVar(&maxmindCountryDBPath, "maxmind-country-db-path", "", "Path to the maxmind country database file")
	rootCmd.PersistentFlags().StringVar(&maxmindASNDBPath, "maxmind-asn-db-path", "", "Path to the maxmind ASN database file")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "How verbose the logs should be. panic, fatal, error, warn, info, debug, trace")
	rootCmd.PersistentFlags().DurationVar(&requestTimeout, "rpc-timeout", 10*time.Second, "How long RPC requests will wait before timing out")
	rootCmd.PersistentFlags().BoolVar(&disableCentralHarvesterCollection, "disable-central-harvester-collection", false, "Disables collection of harvester information via the farmer. Useful for very large farms where this request is very expensive, or cases where chia-exporter is already installed on all harvesters")
	rootCmd.PersistentFlags().BoolVar(&logBlockTimes, "log-block-times", false, "Enables logging of block (pre)validation times to log files.")
	rootCmd.PersistentFlags().StringVar(&mysqlHost, "mysql-host", "127.0.0.1", "MySQL host for metrics that get stored to a DB")
	rootCmd.PersistentFlags().Uint16Var(&mysqlPort, "mysql-port", 3306, "Port of the MySQL database")
	rootCmd.PersistentFlags().StringVar(&mysqlUser, "mysql-user", "root", "The username for the MySQL Database")
	rootCmd.PersistentFlags().StringVar(&mysqlPass, "mysql-password", "password", "The password for the MySQL Database")
	rootCmd.PersistentFlags().StringVar(&mysqlDBName, "mysql-db-name", "chia-exporter", "The database in MySQL to use for metrics")
	rootCmd.PersistentFlags().Uint32Var(&mysqlBatchSize, "mysql-batch-size", 250, "How many records will be batched into a single insert to MySQL")

	cobra.CheckErr(viper.BindPFlag("hostname", rootCmd.PersistentFlags().Lookup("hostname")))
	cobra.CheckErr(viper.BindPFlag("metrics-port", rootCmd.PersistentFlags().Lookup("metrics-port")))
	cobra.CheckErr(viper.BindPFlag("maxmind-country-db-path", rootCmd.PersistentFlags().Lookup("maxmind-country-db-path")))
	cobra.CheckErr(viper.BindPFlag("maxmind-asn-db-path", rootCmd.PersistentFlags().Lookup("maxmind-asn-db-path")))
	cobra.CheckErr(viper.BindPFlag("log-level", rootCmd.PersistentFlags().Lookup("log-level")))
	cobra.CheckErr(viper.BindPFlag("rpc-timeout", rootCmd.PersistentFlags().Lookup("rpc-timeout")))
	cobra.CheckErr(viper.BindPFlag("disable-central-harvester-collection", rootCmd.PersistentFlags().Lookup("disable-central-harvester-collection")))
	cobra.CheckErr(viper.BindPFlag("log-block-times", rootCmd.PersistentFlags().Lookup("log-block-times")))
	cobra.CheckErr(viper.BindPFlag("mysql-host", rootCmd.PersistentFlags().Lookup("mysql-host")))
	cobra.CheckErr(viper.BindPFlag("mysql-port", rootCmd.PersistentFlags().Lookup("mysql-port")))
	cobra.CheckErr(viper.BindPFlag("mysql-user", rootCmd.PersistentFlags().Lookup("mysql-user")))
	cobra.CheckErr(viper.BindPFlag("mysql-password", rootCmd.PersistentFlags().Lookup("mysql-password")))
	cobra.CheckErr(viper.BindPFlag("mysql-db-name", rootCmd.PersistentFlags().Lookup("mysql-db-name")))
	cobra.CheckErr(viper.BindPFlag("mysql-batch-size", rootCmd.PersistentFlags().Lookup("mysql-batch-size")))
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
