package cmd

import (
	"fmt"

	"github.com/fatih/color"
)

func printHeader(format string, a ...interface{}) {
	color.Set(color.FgGreen)
	fmt.Printf("=> ")
	color.Unset()
	fmt.Printf(format, a...)
	fmt.Println()
}

func printList(format string, a ...interface{}) {
	fmt.Print(" - ")
	fmt.Printf(format, a...)
	fmt.Println()
}

func printInfo(format string, a ...interface{}) {
	color.HiCyan(format, a...)
}

func printError(format string, a ...interface{}) {
	color.Red("ERROR ")
	fmt.Printf(format, a...)
	fmt.Println()
}
func printWarn(format string, a ...interface{}) {
	color.Yellow("WARN ")
	fmt.Printf(format, a...)
	fmt.Println()
}
