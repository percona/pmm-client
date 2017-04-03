package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	fmt.Println("This is a test program")
	fmt.Print("What's your name?: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("error reading line")
	}

	// remove '\n'
	name := line[:len(line)-1]

	fmt.Printf("Hi %s, it's nice to meet you!\n", name)
}
