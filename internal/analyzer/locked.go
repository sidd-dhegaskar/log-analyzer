package analyzer

import(
	"errors"
	"sync"
	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

func Locked(files []string)(*accesslog.Stats, error){
	var(
		mu sync.Mutex
		total=accesslog.NewStats()
	)
	errs:=make([]error, len(files))
	var wg sync.WaitGroup
	for i,path:=range files{
		wg.Go(func(){
			s,err:=accesslog.AnalyzeFile(path)
			if err!=nil{
				errs[i]=err
				return
			}
			mu.Lock()
			total.Merge(s)
			mu.Unlock()
	})
	}
	wg.Wait()
	if err:=errors.Join(errs...);err!=nil{
		return nil,err
	}
	return total,nil
}