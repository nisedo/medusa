package colors

import "fmt"

// ColorFunc is an alias type for a coloring function that accepts anything and returns a colorized string
type ColorFunc = func(s any) string

// Reset is a ColorFunc that simply returns the input as a string. It is basically a no-op and is used for resetting the
// color context during complex logging operations.
func Reset(s any) string {
	return fmt.Sprintf("%v", s)
}

// Red is a ColorFunc that returns a red-colorized string of the provided input
func Red(s any) string {
	return Colorize(s, RED)
}

// RedBold is a ColorFunc that returns a red-bold-colorized string of the provided input
func RedBold(s any) string {
	return Colorize(Colorize(s, RED), BOLD)
}

// GreenBold is a ColorFunc that returns a green-bold-colorized string of the provided input
func GreenBold(s any) string {
	return Colorize(Colorize(s, GREEN), BOLD)
}

// YellowBold is a ColorFunc that returns a yellow-bold-colorized string of the provided input
func YellowBold(s any) string {
	return Colorize(Colorize(s, YELLOW), BOLD)
}

// BlueBold is a ColorFunc that returns a blue-bold-colorized string of the provided input
func BlueBold(s any) string {
	return Colorize(Colorize(s, BLUE), BOLD)
}

// MagentaBold is a ColorFunc that returns a magenta-bold-colorized string of the provided input
func MagentaBold(s any) string {
	return Colorize(Colorize(s, MAGENTA), BOLD)
}

// CyanBold is a ColorFunc that returns a cyan-bold-colorized string of the provided input
func CyanBold(s any) string {
	return Colorize(Colorize(s, CYAN), BOLD)
}

// Bold is a ColorFunc that returns a bolded string of the provided input
func Bold(s any) string {
	return Colorize(s, BOLD)
}
