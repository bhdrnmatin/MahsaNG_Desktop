// Command mahsang is the MahsaNG desktop GUI (Windows/Linux), built on Fyne.
package main

import (
	"mahsang/internal/logx"
	"mahsang/internal/ui"
)

func main() {
	logx.Setup()
	ui.New().Run()
}
