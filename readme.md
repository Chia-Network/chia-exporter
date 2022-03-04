# Chia Exporter

Chia Exporter is an application that is intended to run alongside a chia installation and exports prometheus style metrics based on data available from Chia RPCs. Where possible, all data is received as events from websocket subscriptions. Some data that is not available as a metrics event is also fetched as well, but usually in response to an event that was already received that indicates the data may have changed (with the goal to only make as many RPC requests as necessary to get accurate metric data).

**_This project is actively under development and relies on data that may not yet be available in a stable release of Chia Blockchain. Dev builds of chia may contain bugs or other issues that are not present in tagged releases. We do not recommend that you run pre-release/dev versions of Chia Blockchain on mission critical systems._**

## Usage

First, install [chia-blockchain](https://github.com/Chia-Network/chia-blockchain). Chia exporter expects to be run on the same machine as the chia blockchain installation, and will use either the default chia config (`~/.chia/mainnet/`) or else the config located at `CHIA_ROOT`, if the environment variable is set.

`chia-exporter serve` will start the metrics exporter on the default port of `9914`. Metrics will be available at `<hostname>:9914/metrics`.

### Configuration

Configuration options can be passed using command line flags, environment variables, or a configuration file, except for `--config`, which is a CLI flag only. For a complete listing of options, run `chia-exporter --help`.

To set a config value as an environment variable, prefix the name with `CHIA_EXPORTER_`, convert all letters to uppercase, and replace any dashes with underscores (`metrics-port` becomes `CHIA_EXPORTER_METRICS_PORT`).

To use a config file, create a new yaml file and place any configuration options you want to specify in the file. The config file will be loaded by default from `~/.chia-exporter`, but the location can be overridden with the `--config` flag.

```yaml
metrics-port: 9914
```

## Optional Metrics

There are a number of metrics that are opt-in due to external dependencies or because of a higher performance requirement to support them.

### Block Count Metrics

Counts of uncompact and compact blocks results in a database query that does a pretty big read from disk, so can be taxing on lower powered systems, and is disabled by default. To enable this metric, pass `--enable-block-counts`

### Country Data

When running alongside the crawler, the exporter can optionally export metrics indicating how many peers have been discovered in each country, based on IP address. To enable this functionality, you will need to download the MaxMind GeoLite2 Country database and provide the path to the MaxMind database to the exporter application. The path can be provided with a command line flag `--maxmind-db-path /path/to/GeoLite2-Country.mmdb`, an entry in the config yaml file `maxmind-db-path: /path/to/GeoLite2-Country.mmdb`, or an environment variable `CHIA_EXPORTER_MAXMIND_DB_PATH=/path/to/GeoLite2-Country.mmdb`. To gain access to the MaxMind DB, you can [register here](https://www.maxmind.com/en/geolite2/signup).
