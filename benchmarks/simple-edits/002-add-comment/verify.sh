# Check that a comment line exists before func main, and code still builds
grep -B1 'func main' main.go | grep -q '//' && go build -o /dev/null main.go 2>/dev/null
