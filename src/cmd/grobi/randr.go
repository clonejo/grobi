package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
)

// Output encapsulates a physical output with detected modes.
type Output struct {
	Name      string
	Modes     Modes
	Connected bool
}

func (o Output) String() string {
	var con string
	if o.Connected {
		con = " (connected)"
	}
	str := fmt.Sprintf("%s%s", o.Name, con)
	if len(o.Modes) > 0 {
		str += fmt.Sprintf(" %v", o.Modes)
	}
	return str
}

// Equals checks whether the two Outputs are equal.
func (o Output) Equals(other Output) bool {
	if o.Name != other.Name || o.Connected != other.Connected {
		return false
	}

	if len(o.Modes) != len(other.Modes) {
		return false
	}

	for i := range o.Modes {
		m1 := o.Modes[i]
		m2 := other.Modes[i]

		if m1 != m2 {
			return false
		}
	}

	return true
}

// Outputs is a list of outputs.
type Outputs []Output

// Present returns true iff the list of outputs contains the named output.
func (os Outputs) Present(name string) bool {
	for _, o := range os {
		m, err := path.Match(name, o.Name)
		if err != nil {
			return false
		}

		if m {
			return true
		}
	}
	return false
}

// Connected returns true iff the list of outputs contains the named output and
// it is connected.
func (os Outputs) Connected(name string) bool {
	for _, o := range os {
		m, err := path.Match(name, o.Name)
		if err != nil {
			return false
		}

		if m && o.Connected {
			return true
		}
	}
	return false
}

// Equals checks whether the two Outputs are equal.
func (os Outputs) Equals(other Outputs) bool {
	if len(os) != len(other) {
		return false
	}

	for i := range os {
		out1 := os[i]
		out2 := other[i]

		if !out1.Equals(out2) {
			return false
		}
	}

	return true
}

// Mode is an output mode that may be active or default.
type Mode struct {
	Name    string
	Default bool
	Active  bool
}

func (m Mode) String() string {
	var suffix string

	if m.Active {
		suffix += "*"
	}

	if m.Default {
		suffix += "+"
	}

	return m.Name + suffix
}

// Modes is a list of Mode.
type Modes []Mode

func (m Modes) String() string {
	var str []string
	for _, mode := range m {
		str = append(str, mode.String())
	}
	return strings.Join(str, " ")
}

// errNotModeLine is returned by parseModeLine when the line doesn't match
// the format for a mode line.
var errNotModeLine = errors.New("not a mode line")

// parseOutputLine returns the output parsed from the string.
func parseOutputLine(line string) (Output, error) {
	output := Output{}

	ws := bufio.NewScanner(bytes.NewReader([]byte(line)))
	ws.Split(bufio.ScanWords)

	if !ws.Scan() {
		return Output{}, fmt.Errorf("line too short, name not found: %s", line)
	}
	output.Name = ws.Text()

	if !ws.Scan() {
		return Output{}, fmt.Errorf("line too short, state not found: %s", line)
	}

	switch ws.Text() {
	case "connected":
		output.Connected = true
	case "disconnected":
		output.Connected = false
	default:
		return Output{}, fmt.Errorf("unknown state %q", ws.Text())
	}

	// handle special case: output is disconnected, but still active
	if output.Connected || !ws.Scan() {
		return output, nil
	}

	arg := strings.Split(ws.Text(), "+")
	if len(arg) != 3 {
		return output, nil
	}

	mode := arg[0]
	output.Modes = append(output.Modes, Mode{Name: mode, Active: true})

	return output, nil
}

// parseModeLine returns the mode parsed from the string.
func parseModeLine(line string) (mode Mode, err error) {
	if !strings.HasPrefix(line, "  ") {
		return Mode{}, errNotModeLine
	}

	ws := bufio.NewScanner(bytes.NewReader([]byte(line)))
	ws.Split(bufio.ScanWords)

	if !ws.Scan() {
		return Mode{}, fmt.Errorf("line too short, mode name not found: %s", line)
	}
	mode.Name = ws.Text()

	if !ws.Scan() {
		return Mode{}, fmt.Errorf("line too short, no refresh rate found: %s", line)
	}
	rate := ws.Text()

	if rate[len(rate)-1] == '+' {
		mode.Default = true
	}

	if rate[len(rate)-2] == '*' {
		mode.Active = true
	}

	// handle single-word "+", which happens when a mode is default but not active
	if ws.Scan() && ws.Text() == "+" {
		mode.Default = true
	}

	return mode, nil
}

// RandrParse returns the list of outputs parsed from the reader.
func RandrParse(rd io.Reader) (outputs Outputs, err error) {
	ls := bufio.NewScanner(rd)

	const (
		StateStart = iota
		StateOutput
		StateMode
	)

	var (
		state  = StateStart
		output Output
	)

nextLine:
	for ls.Scan() {
		line := ls.Text()

		for {
			switch state {
			case StateStart:
				if strings.HasPrefix(line, "Screen ") {
					state = StateOutput
					continue nextLine
				}
				return nil, fmt.Errorf(`first line should start with "Screen", found: %v`, line)

			case StateOutput:
				output, err = parseOutputLine(line)
				if err != nil {
					return nil, err
				}
				state = StateMode
				continue nextLine

			case StateMode:
				mode, err := parseModeLine(line)
				if err == errNotModeLine {
					outputs = append(outputs, output)
					output = Output{}
					state = StateOutput
					continue
				}

				if err != nil {
					return nil, err
				}

				output.Modes = append(output.Modes, mode)
				continue nextLine
			}
		}
	}

	if output.Name != "" {
		outputs = append(outputs, output)
	}

	return outputs, nil
}

func runXrandr(extraArgs ...string) *exec.Cmd {
	args := []string{"--query"}
	args = append(args, extraArgs...)
	cmd := exec.Command("xrandr", args...)
	cmd.Stderr = os.Stderr
	return cmd
}

// GetOutputs runs `xrandr` and returns the parsed output.
func GetOutputs() (Outputs, error) {
	cmd := runXrandr("--current")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return RandrParse(bytes.NewReader(output))
}

// DetectOutputs runs `xrandr`, rescans the outputs and returns the parsed outputs.
func DetectOutputs() (Outputs, error) {
	cmd := runXrandr()
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return RandrParse(bytes.NewReader(output))
}

// BuildCommandOutputRow return a sequence of calls to `xrandr` to configure
// all named outputs in a row, left to right, given the currently active
// Outputs and a list of output names, optionally followed by "@" and the
// desired mode, e.g. LVDS1@1377x768.
func BuildCommandOutputRow(rule Rule, current Outputs) ([]*exec.Cmd, error) {
	var outputs []string

	switch {
	case rule.ConfigureSingle != "":
		outputs = []string{rule.ConfigureSingle}
	case len(rule.ConfigureRow) > 0:
		outputs = rule.ConfigureRow
	default:
		return nil, errors.New("empty monitor row configuration")
	}

	verbosePrintf("enable outputs: %v\n", outputs)

	command := "xrandr"
	enableOutputArgs := [][]string{}

	active := make(map[string]struct{})
	var lastOutput = ""
	for i, output := range outputs {
		data := strings.SplitN(output, "@", 2)
		name := data[0]
		mode := ""
		if len(data) > 1 {
			mode = data[1]
		}

		active[name] = struct{}{}

		args := []string{}
		args = append(args, "--output", name)
		if mode == "" {
			args = append(args, "--auto")
		} else {
			args = append(args, "--mode", mode)
		}

		if i > 0 {
			args = append(args, "--right-of", lastOutput)
		}

		lastOutput = name
		enableOutputArgs = append(enableOutputArgs, args)
	}

	disableOutputs := make(map[string]struct{})
	for _, output := range current {
		if !output.Connected && len(output.Modes) == 0 {
			continue
		}

		// disable unneeded outputs that are still active
		if _, ok := active[output.Name]; !ok {
			disableOutputs[output.Name] = struct{}{}
		}
	}

	disableOutputArgs := [][]string{}

	// honour disable_order if present
	for _, name := range rule.DisableOrder {
		if _, ok := disableOutputs[name]; ok {
			args := []string{"--output", name, "--off"}
			disableOutputArgs = append(disableOutputArgs, args)

			delete(disableOutputs, name)
		}
	}

	// collect remaining outputs to be disabled
	for name := range disableOutputs {
		args := []string{"--output", name, "--off"}
		disableOutputArgs = append(disableOutputArgs, args)
	}

	// enable/disable all monitors in one call to xrandr
	if rule.Atomic {
		verbosePrintf("using one atomic call to xrandr\n")
		args := []string{}
		for _, disableArgs := range disableOutputArgs {
			args = append(args, disableArgs...)
		}
		for _, enableArgs := range enableOutputArgs {
			args = append(args, enableArgs...)
		}
		cmd := exec.Command(command, args...)
		return []*exec.Cmd{cmd}, nil
	}

	verbosePrintf("splitting the configuration into several calls to xrandr\n")

	// otherwise return several calls to xrandr
	cmds := []*exec.Cmd{}

	// disable an output
	if len(disableOutputArgs) > 0 {
		cmds = append(cmds, exec.Command(command, disableOutputArgs[0]...))
		disableOutputArgs = disableOutputArgs[1:]
	}

	// now for each newly enabled output, also disable another output
	for len(disableOutputArgs) > 0 || len(enableOutputArgs) > 0 {
		args := []string{}
		if len(disableOutputArgs) > 0 {
			args = append(args, disableOutputArgs[0]...)
			disableOutputArgs = disableOutputArgs[1:]
		}
		if len(enableOutputArgs) > 0 {
			args = append(args, enableOutputArgs[0]...)
			enableOutputArgs = enableOutputArgs[1:]
		}

		cmds = append(cmds, exec.Command(command, args...))
	}

	return cmds, nil
}
