package main

import "fmt"

const (
	ESC         = "\033"
	CSI         = ESC + "["
	HideCursor  = CSI + "?25l"
	ShowCursor  = CSI + "?25h"
	ClearScreen = CSI + "2J"
	HomeCursor  = CSI + "H"
	ExitSixel   = ESC + "\\"
	EnterSixel  = ESC + "P"
	ClearLine   = CSI + "2K"
)

func MoveCursor(row, col int) string {
	return fmt.Sprintf("%s%d;%dH", CSI, row, col)
}
