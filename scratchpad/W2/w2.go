package main

import(
	"fmt"
	"sync"
)

func main(){
	var wg sync.WaitGroup
	for i:= range 3{
		wg.Go(func(){
			fmt.Println("worker",i,"done")
		})
	}
	wg.Wait()
	fmt.Println("All done")
}
