package analyzer

import(
	"fmt"
	"os"
	"path/filepath"
	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
	"sync"
)

func Racy(files []string)(*accesslog.Stats, error){
	total:= accesslog.NewStats()
	var wg sync.WaitGroup
	for _,path:=range files{
		wg.Add(1)
		go func(){
			defer wg.Done()
			s,err:=accesslog.AnalyzeFile(path)
			if err!=nil{
				return
			}
			fmt.Fprintf(os.Stderr,"%s: %d requests\n", filepath.Base(path),s.Requests)
		}()
	}
	wg.Wait()
	return total,nil
}
