package main

import(
	"fmt"
	"flag"
	"os"
	"github.com/sidd-dhegaskar/log-analyzer/internal/loggen"
)

func main(){
	dir:=flag.String("dir","logs","output directory")
	files:=flag.Int("files",1000,"Number of files")
	lines:=flag.Int("lines",500,"Number of lines per file")
	seed:=flag.Uint64("seed",1,"random seed")
	flag.Parse()

	if err:=loggen.Generate(*dir,*files,*lines,*seed);err!=nil{
		fmt.Fprintln(os.Stderr,"loggen:",err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %d files x %d lines to %s\n", *files, *lines, *dir)
}