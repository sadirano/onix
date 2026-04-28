package config

// Shortcuts maps executable basenames to their implicit action flag.
// Used at runtime for basename injection and by the installer when
// generating .cmd wrappers.
var Shortcuts = map[string]string{
	"o":   "",
	"c":   "",
	"s":   "-e",
	"n":   "-n",
	"y":   "-y",
	"f":   "-f",
	"r":   "-r",
	"sg":  "-sg",
	"sga": "-sga",
	"ff":  "-ff",
}
