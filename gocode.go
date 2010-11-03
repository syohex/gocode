package main

import (
	"io/ioutil"
	"strings"
	"strconv"
	"exec"
	"rpc"
	"flag"
	"time"
	"path"
	"fmt"
	"os"
	"json"
)

var (
	server = flag.Bool("s", false, "run a server instead of a client")
	format = flag.String("f", "nice", "output format (vim | emacs | nice | csv)")
	input  = flag.String("in", "", "use this file instead of stdin input")
)

//-------------------------------------------------------------------------
// Formatter interface
//-------------------------------------------------------------------------

type Formatter interface {
	WriteEmpty()
	WriteCandidates(names, types, classes []string, num int)
	WriteSMap(decldescs []DeclDesc)
	WriteRename(renamedescs []RenameDesc, err string)
}

//-------------------------------------------------------------------------
// NiceFormatter (just for testing, simple textual output)
//-------------------------------------------------------------------------

type NiceFormatter struct{}

func (*NiceFormatter) WriteEmpty() {
	fmt.Printf("Nothing to complete.\n")
}

func (*NiceFormatter) WriteCandidates(names, types, classes []string, num int) {
	fmt.Printf("Found %d candidates:\n", len(names))
	for i := 0; i < len(names); i++ {
		abbr := fmt.Sprintf("%s %s %s", classes[i], names[i], types[i])
		if classes[i] == "func" {
			abbr = fmt.Sprintf("%s %s%s", classes[i], names[i], types[i][len("func"):])
		}
		fmt.Printf("  %s\n", abbr)
	}
}

func (*NiceFormatter) WriteSMap(decldescs []DeclDesc) {
	data, err := json.Marshal(decldescs)
	if err != nil {
		panic(err.String())
	}
	os.Stdout.Write(data)
}

func (*NiceFormatter) WriteRename(renamedescs []RenameDesc, err string) {
	data, error := json.Marshal(renamedescs)
	if error != nil {
		panic(error.String())
	}
	os.Stdout.Write(data)
}

//-------------------------------------------------------------------------
// VimFormatter
//-------------------------------------------------------------------------

type VimFormatter struct{}

func (*VimFormatter) WriteEmpty() {
	fmt.Print("[0, []]")
}

func (*VimFormatter) WriteCandidates(names, types, classes []string, num int) {
	fmt.Printf("[%d, [", num)
	for i := 0; i < len(names); i++ {
		word := names[i]
		if classes[i] == "func" {
			word += "("
		}

		abbr := fmt.Sprintf("%s %s %s", classes[i], names[i], types[i])
		if classes[i] == "func" {
			abbr = fmt.Sprintf("%s %s%s", classes[i], names[i], types[i][len("func"):])
		}
		fmt.Printf("{'word': '%s', 'abbr': '%s'}", word, abbr)
		if i != len(names)-1 {
			fmt.Printf(", ")
		}

	}
	fmt.Printf("]]")
}

func (*VimFormatter) WriteSMap(decldescs []DeclDesc) {
}

func vimQuote(s string) string {
	s = strings.Replace(s, "'", "''", -1)
	return s
}

func (*VimFormatter) WriteRename(renamedescs []RenameDesc, err string) {
	if err != "" {
		fmt.Printf("['%s', []]", vimQuote(err))
		return
	}
	if renamedescs == nil {
		fmt.Print("['Nothing to rename', []]")
		return
	}
	fmt.Print("['OK', [")
	for i, r := range renamedescs {
		fmt.Printf("{'filename':'%s','length':%d,'decls':", r.Filename, r.Length)
		fmt.Print("[")
		for j, d := range r.Decls {
			fmt.Printf("[%d,%d]", d.Line, d.Col)
			if j != len(r.Decls)-1 {
				fmt.Print(",")
			}
		}
		fmt.Print("]")
		fmt.Print("}")
		if i != len(renamedescs)-1 {
			fmt.Print(",")
		}
	}
	fmt.Print("]]")
}

//-------------------------------------------------------------------------
// EmacsFormatter
//-------------------------------------------------------------------------

type EmacsFormatter struct{}

func (*EmacsFormatter) WriteEmpty() {
}

func (*EmacsFormatter) WriteCandidates(names, types, classes []string, num int) {
	for i := 0; i < len(names); i++ {
		name := names[i]
		hint := classes[i] + " " + types[i]
		if classes[i] == "func" {
			hint = types[i]
		}
		fmt.Printf("%s,,%s\n", name, hint)
	}
}

func (*EmacsFormatter) WriteSMap(decldescs []DeclDesc) {
}

func (*EmacsFormatter) WriteRename(renamedescs []RenameDesc, err string) {
}

//-------------------------------------------------------------------------
// CSVFormatter
//-------------------------------------------------------------------------

type CSVFormatter struct{}

func (*CSVFormatter) WriteEmpty() {
}

func (*CSVFormatter) WriteCandidates(names, types, classes []string, num int) {
	for i := 0; i < len(names); i++ {
		fmt.Printf("%s,,%s,,%s\n", classes[i], names[i], types[i])
	}
}

func (*CSVFormatter) WriteSMap(decldescs []DeclDesc) {
}

func (*CSVFormatter) WriteRename(renamedescs []RenameDesc, err string) {
}

//-------------------------------------------------------------------------

func getFormatter() Formatter {
	switch *format {
	case "vim":
		return new(VimFormatter)
	case "emacs":
		return new(EmacsFormatter)
	case "nice":
		return new(NiceFormatter)
	case "csv":
		return new(CSVFormatter)
	}
	return new(VimFormatter)
}

func getSocketFilename() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "all"
	}
	return fmt.Sprintf("%s/acrserver.%s", os.TempDir(), user)
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return true
}

func serverFunc() int {
	readConfig(&Config)
	socketfname := getSocketFilename()
	if fileExists(socketfname) {
		fmt.Printf("unix socket: '%s' already exists\n", socketfname)
		return 1
	}
	daemon = NewDaemon(socketfname)
	defer os.Remove(socketfname)

	rpcremote := new(RPCRemote)
	rpc.Register(rpcremote)

	daemon.acr.Loop()
	return 0
}

func cmdStatus(c *rpc.Client) {
	fmt.Printf("%s\n", Client_Status(c, 0))
}

func cmdAutoComplete(c *rpc.Client) {
	var file []byte
	var err os.Error

	if *input != "" {
		file, err = ioutil.ReadFile(*input)
	} else {
		file, err = ioutil.ReadAll(os.Stdin)
	}

	if err != nil {
		panic(err.String())
	}

	filename := ""
	cursor := -1

	switch flag.NArg() {
	case 2:
		cursor, _ = strconv.Atoi(flag.Arg(1))
	case 3:
		filename = flag.Arg(1)
		cursor, _ = strconv.Atoi(flag.Arg(2))
	}

	if filename != "" && filename[0] != '/' {
		cwd, _ := os.Getwd()
		filename = path.Join(cwd, filename)
	}

	formatter := getFormatter()
	names, types, classes, partial := Client_AutoComplete(c, file, filename, cursor)
	if names == nil {
		formatter.WriteEmpty()
		return
	}

	formatter.WriteCandidates(names, types, classes, partial)
}

func cmdSMap(c *rpc.Client) {
	if flag.NArg() != 2 {
		return
	}

	filename := flag.Arg(1)
	if filename != "" && filename[0] != '/' {
		cwd, _ := os.Getwd()
		filename = path.Join(cwd, filename)
	}

	formatter := getFormatter()
	decldescs := Client_SMap(c, filename)

	formatter.WriteSMap(decldescs)
}

func cmdRename(c *rpc.Client) {
	if flag.NArg() != 3 {
		return
	}

	cursor := 0
	filename := flag.Arg(1)
	cursor, _ = strconv.Atoi(flag.Arg(2))

	if filename != "" && filename[0] != '/' {
		cwd, _ := os.Getwd()
		filename = path.Join(cwd, filename)
	}

	formatter := getFormatter()
	renamedescs, err := Client_Rename(c, filename, cursor)

	formatter.WriteRename(renamedescs, err)
}

func cmdClose(c *rpc.Client) {
	Client_Close(c, 0)
}

func cmdDropCache(c *rpc.Client) {
	Client_DropCache(c, 0)
}

func cmdSet(c *rpc.Client) {
	switch flag.NArg() {
	case 1:
		fmt.Print(Client_Set(c, "", ""))
	case 2:
		fmt.Print(Client_Set(c, flag.Arg(1), ""))
	case 3:
		fmt.Print(Client_Set(c, flag.Arg(1), flag.Arg(2)))
	}
}

func makeFDs() ([]*os.File, os.Error) {
	var fds [3]*os.File
	var err os.Error
	fds[0], err = os.Open("/dev/null", os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	fds[1], err = os.Open("/dev/null", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	fds[2], err = os.Open("/dev/null", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	// I know that technically it's possible here that there will be unclosed
	// file descriptors on exit. But since that kind of error will result in
	// a process shutdown anyway, I don't care much about that.

	return fds[:], nil
}

func tryRunServer() os.Error {
	fds, err := makeFDs()
	if err != nil {
		return err
	}
	defer fds[0].Close()
	defer fds[1].Close()
	defer fds[2].Close()

	var path string
	path, err = exec.LookPath("gocode")
	if err != nil {
		return err
	}

	_, err = os.ForkExec(path, []string{"gocode", "-s"}, os.Environ(), "", fds)
	if err != nil {
		return err
	}
	return nil
}

func waitForAFile(fname string) {
	t := 0
	for !fileExists(fname) {
		time.Sleep(10000000) // 0.01
		t += 10
		if t > 1000 {
			return
		}
	}
}

func clientFunc() int {
	socketfname := getSocketFilename()

	// client
	client, err := rpc.Dial("unix", socketfname)
	if err != nil {
		err = tryRunServer()
		if err != nil {
			fmt.Printf("%s\n", err.String())
			return 1
		}
		waitForAFile(socketfname)
		client, err = rpc.Dial("unix", socketfname)
		if err != nil {
			fmt.Printf("%s\n", err.String())
			return 1
		}
	}
	defer client.Close()

	if flag.NArg() > 0 {
		switch flag.Arg(0) {
		case "autocomplete":
			cmdAutoComplete(client)
		case "close":
			cmdClose(client)
		case "status":
			cmdStatus(client)
		case "drop-cache":
			cmdDropCache(client)
		case "set":
			cmdSet(client)
		case "smap":
			cmdSMap(client)
		case "rename":
			cmdRename(client)
		}
	}
	return 0
}

func main() {
	flag.Parse()

	var retval int
	if *server {
		retval = serverFunc()
	} else {
		retval = clientFunc()
	}
	os.Exit(retval)
}
