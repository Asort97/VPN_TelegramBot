package colorfulprint

import "fmt"

const (
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorWhite  = "\033[37m"
	ColorReset  = "\033[0m"
)

func PrintError(text string, err error) error {
	coloredText := ColorRed + text + ColorReset + "\n"
	fmt.Println(coloredText, err)
	return fmt.Errorf(coloredText, err)
}

func PrintState(text string) {
	coloredText := ColorGreen + text + ColorReset
	fmt.Println(coloredText)
}
