package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/tiborvass/go-translate"
)

func init() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), `Usage: translate [-from <source_lang=auto>] -to <target_lang> [...files]`)
		fmt.Fprintln(flag.CommandLine.Output(), `       translate -langs`)
	}
}

var from = flag.String("from", "auto", "Source language")
var to = flag.String("to", "", "Target language")
var langs = flag.Bool("langs", false, "Print list of available languages")

func main() {
	flag.Parse()

	if *langs {
		list, err := translate.Languages()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		tab := tabwriter.NewWriter(os.Stdout, 0, 4, 1, ' ', 0)
		for _, e := range list {
			fmt.Fprintf(tab, "%s\t%s\t%s\n", e.Code, e.EnglishName, e.Endonym)
		}
		tab.Flush()
		return
	}
	if *to == "" {
		flag.Usage()
		return
	}

	readers := make([]io.Reader, flag.NArg())
	hadStdin := false
	for _, file := range flag.Args() {
		if file == "-" {
			if hadStdin {
				fmt.Fprintln(os.Stderr, `stdin ("-") already specified`)
				return
			}
			readers = append(readers, os.Stdin)
			hadStdin = true
			continue
		}
		f, err := os.Open(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		defer f.Close()
		readers = append(readers, f)
	}
	if len(readers) == 0 {
		readers = []io.Reader{os.Stdin}
	}

	err := translate.Translate(io.MultiReader(readers...), os.Stdout, *from, *to)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
