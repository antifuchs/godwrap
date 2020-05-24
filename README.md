# `godwrap` - a cronwrapper implementation that ties into influxdb

`godwrap` is a very basic cron wrapper tool:

* It captures job output and only says something if the program exited with non-zero
* It records job states and emits those jobs' states as influxdb metrics.
* It allows administrators to retrieve the output and details of each job's last run.

In short, it's a replacement for `dogwrap.py` that doesn't depend on
datadog or statsd metrics ingestion.

## Installation

`go get github.com/antifuchs/godwrap`

Then ensure that your state file directory exists. The default is `/var/lib/godwrap`.

## Usage

### Running your cronjob

``` sh
godwrap run --name="name-of-cronjob" /usr/bin/program arg1 arg2 "arg three"
```

This will execute the program, capture output and write it all to the state file `/var/lib/godwrap/name-of-cronjob.json`.

### Capturing metrics on your cronjobs

Set up telegraf with the following configuration:

``` toml
[[inputs.execd]]
  command = ["/path/to/godwrap", "influxdb", "--execd"]
  data_format = "influx"
  signal = "STDIN"
```

This will output the following metrics, tagged with `name` (the name given to the cronjob above):

* `godwrap_cronjob.exit_status` integer - the exit code from the cronjob. Assume this is a problem if that is non-0.
* `godwrap_cronjob.success` boolean - true if the exit code is 0, false otherwise
* `godwrap_cronjob.last_run` int64 - timestamp of the last run, in number of seconds since the UNIX epoch.

### Inspecting your cronjobs' last run

```sh
godwrap inspect --debug /var/lib/godwrap/name-of-cronjob.json
```

will output something like:

```
job="name-of-cronjob" ran=2020-05-23 18:23:38.143064 -0400 EDT cmdline="[bash -c echo welp; echo \"no\" >&2; echo yeah; false]" error="exit status 1" success=false exit_status=1
output:
welp
no
yeah
```