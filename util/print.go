package util

import (
	"fmt"
	"os"

	"github.com/fatih/color"
)

func LogInfoHeader(format string, a ...interface{}) {
	color.Set(color.FgGreen)
	fmt.Printf("=> ")
	color.Unset()
	fmt.Printf(format, a...)
	fmt.Println()
}

func LogInfoListItem(format string, a ...interface{}) {
	fmt.Print(" - ")
	fmt.Printf(format, a...)
	fmt.Println()
}

func LogInfo(format string, a ...interface{}) {
	color.HiCyan(format, a...)
}

func LogError(format string, a ...interface{}) {
	red := color.New(color.FgRed)
	red.Fprint(os.Stderr, "ERROR ")
	fmt.Fprintf(os.Stderr, format, a...)
	fmt.Fprintln(os.Stderr)
}
