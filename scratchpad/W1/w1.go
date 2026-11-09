package main

import (
	"fmt"
	"time"
)

func main(){
	for i:= range 3{
		go fmt.Println("Hello from go routine",i)		
	}
	time.Sleep(100*time.Millisecond)
	fmt.Println("Hello from main")	
}