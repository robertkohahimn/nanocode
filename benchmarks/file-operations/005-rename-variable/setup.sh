go mod init benchmark-test
cat > main.go << 'EOF'
package main

import "fmt"

func main() {
	foo := "hello"
	fmt.Println(foo)
	foo = foo + " world"
	fmt.Println(foo)
}
EOF
