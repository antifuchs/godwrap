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
godwrap run --name="name-of-cronjob" -- /usr/bin/program arg1 arg2 "arg three"
```

This will execute the program, capture output and write it all to the state file `/var/lib/godwrap/35b105de417f23876a4d5d4e93ea540b8a3666ab.json`.

#### Caution on the `--` separator

You *can* pass the program and its args to godwrap without using the
`--` command separator, but it's unsafe: If the command line to invoke
includes a `-` flag, `godwrap`'s command line parser will try and
interpret it (see [this
issue](https://github.com/alecthomas/kong/issues/80)).

Any command might grow a more complex command line eventually, so it's
safest to always separate the godwrap command line from the wrapped
command's command line with a `--`.

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

### Inspecting your cronjobs' last run

```sh
godwrap inspect --debug name-of-cronjob.json
```

or

```sh
godwrap inspect --debug /var/lib/godwrap/35b105de417f23876a4d5d4e93ea540b8a3666ab.json
```

will output something like:

```
job="name-of-cronjob" ran=2020-05-23 18:23:38.143064 -0400 EDT cmdline="[bash -c echo welp; echo \"no\" >&2; echo yeah; false]" error="exit status 1" success=false exit_status=1
output:
welp
no
yeah
```
