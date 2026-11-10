package analyzer

import(
	"errors"
	"sync"
	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

// Racy starts one goroutine per file and merges into shared Stats WITHOUT
// synchronization.

func Racy(files []string)(*accesslog.Stats, error){
	total:=accesslog.NewStats()
	errs:= make([]error,len(files))
	var wg sync.WaitGroup
	for i, path:=range files{
		wg.Add(1)
		go func(){
			defer wg.Done()
			s,err:=accesslog.AnalyzeFile(path)
			if err!=nil{
				errs[i]=err
				return
			}
			total.Merge(s)
		}()
	}
	wg.Wait()
	if err:=errors.Join(errs...);err!=nil{
		return nil,err
	}
	return total,nil
}