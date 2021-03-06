// godwrap wraps a cronjob and stores its result (success or failure),
// along with its output, in a state directory.
package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/google/renameio"
)

type Context struct {
	Debug     bool
	StatusDir string
}

type Run struct {
	Name    string        `help:"Name of this cron job. Must be unique."`
	Timeout time.Duration `help:"Maximum amount of time that the job can run."`
	Mode    string        `help:"File mode (in guessed base, so prefix with 0) for the status file." default:"0640"`
	Command []string      `arg help:"Command (and arguments) to run"`
}

func (r *Run) fileMode() os.FileMode {
	modeInt, err := strconv.ParseUint(r.Mode, 0, 32)
	if err != nil {
		log.Fatalf("Can't parse file mode %q: %v", r.Mode, err)
	}
	mode := os.FileMode(modeInt) & os.ModePerm
	return mode
}

// Run on a Run runs the cronjob.
func (r *Run) Run(cctx *Context) error {
	ctx := context.Background()
	mode := r.fileMode()
	if r.Timeout > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	name := r.Name
	if name == "" {
		name = strings.Join(r.Command, " ")
	}
	cmd := exec.CommandContext(ctx, r.Command[0], r.Command[1:]...)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	wrote := output.String()
	if err != nil {
		log.Printf("Cronjob %q (%v) failed: %v.\nOutput follows:\n\n%v", name, r.Command, err, wrote)
	}
	statusErr := writeStatus(cctx, name, r.Command, wrote, mode, err)
	if statusErr != nil {
		log.Printf("Could not write status file for %q: %v", name, statusErr)
		return statusErr
	}
	return err
}

type statusJSON struct {
	Name        string    `json:"name"`
	LastRun     time.Time `json:"last_run"`
	CommandLine []string  `json:"command_line"`
	Username    string    `json:"user_name"`
	Uid         string    `json:"user_id"`
	Environ     []string  `json:"environment"`
	Output      string    `json:"output"`
	Error       string    `json:"error"`
	ExitStatus  int       `json:"exit_status"`
	Success     bool      `json:"success"`
}

func statusFileName(dir, name string) string {
	sum := sha1.Sum([]byte(name))
	return filepath.Join(dir, fmt.Sprintf("%x.json", sum))
}

func writeStatus(cctx *Context, name string, commandLine []string, output string, mode os.FileMode, status error) error {
	filename := statusFileName(cctx.StatusDir, name)
	statusContents := statusJSON{
		Name:        name,
		LastRun:     time.Now(),
		Environ:     os.Environ(),
		Output:      output,
		CommandLine: commandLine,
		Success:     status == nil,
	}
	if status != nil {
		statusContents.Error = status.Error()
	}
	if ee, ok := status.(*exec.ExitError); ok {
		statusContents.ExitStatus = ee.ExitCode()
	} else if status != nil {
		statusContents.ExitStatus = -17 // some negative number that indicates it's nonsense
	}
	user, err := user.Current()
	if err == nil {
		// Only fill in the fields if retrieving the user worked:
		statusContents.Username = user.Username
		statusContents.Uid = user.Uid
	}
	file, err := renameio.TempFile("", filename)
	if err != nil {
		return err
	}
	defer file.Cleanup()

	enc := json.NewEncoder(file)
	enc.Encode(statusContents)
	os.Chmod(file.Name(), mode)
	return file.CloseAtomicallyReplace()
}

type InfluxDB struct {
	Measurement string `help:"Name of the influxdb measurement" default:"godwrap_cronjob"`
	Execd       bool   `help:"Run under execd: Wait until newline is read on stdin, forever."`
}

func (influxdb *InfluxDB) Run(cctx *Context) error {
	buf := make([]byte, 256)
	for {
		statuses, err := filepath.Glob(filepath.Join(cctx.StatusDir, "*.json"))
		if err != nil {
			return err
		}
		for _, status := range statuses {
			actual, err := readOne(status)
			if err != nil {
				log.Fatalf("Could not read status %q: %v", status, err)
			}
			fmt.Printf("%s,name=%q,status_file=%q,user_name=%q,uid=%q exit_status=%di,success=%v %d\n",
				influxdb.Measurement,
				actual.Name,
				status,
				actual.Username,
				actual.Uid,
				actual.ExitStatus,
				actual.Success,
				actual.LastRun.UnixNano(),
			)
		}
		if influxdb.Execd {
			_, err := os.Stdin.Read(buf)
			if err != nil {
				return err
			}
		} else {
			break
		}
	}
	return nil
}

func readOne(status string) (statusJSON, error) {
	var actual statusJSON
	f, err := os.Open(status)
	if err != nil {
		return actual, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	err = dec.Decode(&actual)
	return actual, err
}

type Inspect struct {
	Names []string `arg help:"Names of cron jobs to inspect, or path name to their state file"`
}

func (in *Inspect) Run(cctx *Context) error {
	for _, basename := range in.Names {
		status := basename
		if _, err := os.Stat(status); err != nil && os.IsNotExist(err) {
			status = statusFileName(cctx.StatusDir, basename)
		}
		actual, err := readOne(status)
		if err != nil {
			log.Fatalf("Could not read status %q: %v", status, err)
		}
		fmt.Printf("job=%q status_file=%q user_name=%q user_id=%q ran=%v cmdline=%q error=%q success=%v exit_status=%d\n",
			actual.Name,
			status,
			actual.Username,
			actual.Uid,
			actual.LastRun,
			fmt.Sprintf("%v", actual.CommandLine),
			actual.Error,
			actual.Success,
			actual.ExitStatus,
		)
		if cctx.Debug {
			fmt.Printf("env:\n")
			for _, env := range actual.Environ {
				fmt.Println(env)
			}
			fmt.Printf("\noutput:\n%s", actual.Output)
		}
	}
	return nil
}

var cli struct {
	Run      Run      `cmd help:"Run a cronjob"`
	InfluxDB InfluxDB `cmd name:"influxdb" help:"Emits influxdb metrics for telegraf's 'execd' STDIN collection"`
	Inspect  Inspect  `cmd help:"Outputs information about cron jobs' last run"`

	Debug  bool   `help:"Run in verbose mode"`
	Status string `help:"Directory in which to write status" default:"/var/lib/godwrap"`
}

func main() {
	ctx := kong.Parse(&cli)
	// Call the Run() method of the selected parsed command.
	err := ctx.Run(&Context{
		Debug:     cli.Debug,
		StatusDir: cli.Status,
	})
	ctx.FatalIfErrorf(err)
}
