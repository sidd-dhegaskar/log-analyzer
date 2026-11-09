package loggen

import(
	"bufio" //buffered input output
	"fmt"
	"math/rand/v2" //random number
	"os"
	"path/filepath"
	"time"
)

var endpoints = []string{
	"GET /api/users", "GET /api/orders", "POST /api/orders", "GET /api/products",
	"GET /api/cart", "POST /api/cart", "GET /health", "POST /api/login",
	"GET /api/search", "DELETE /api/cart", "PUT /api/users", "GET /api/reviews",
	"POST /api/payments", "GET /api/inventory", "GET /static/app.js",
}

var statuses = []int{200, 200, 200, 200, 200, 200, 201, 204, 301, 400, 401, 404, 404, 500, 503}

func Generate(dir string, files, lines int, seed uint64) error{
	if err:=os.MkdirAll(dir,0o755);err!=nil{
		return err
	}
	for i :=range files{
		path:= filepath.Join(dir, fmt.Sprintf("access-%04d.log",i))
		r:= rand.New(rand.NewPCG(seed, uint64(i)))
		if err:=writeFile(path,lines,r);err!=nil{
			return fmt.Errorf("generate %s: %w", path, err)
		}
	}
	return nil
}

func writeFile(path string, lines int, r *rand.Rand) error {
	f, err := os.Create(path)
	if err!=nil{
		return err
	}
	w:=bufio.NewWriter(f)
	start:=time.Date(2026,6,9,10,0,0,0, time.UTC)
	for n:= range lines{
		if r.IntN(1000)==0{
			fmt.Fprintln(w,"corrupted line")
			continue
		}
		ts:= start.Add(time.Duration(n) * time.Second).Format(time.RFC3339)
		ep := endpoints[r.IntN(len(endpoints))]
		status := statuses[r.IntN(len(statuses))]
		latency := 1 + r.IntN(500)
		fmt.Fprintf(w, "%s %s %d %dms\n", ts, ep, status, latency)
	}
	if err:=w.Flush();err!=nil{
		f.Close()
		return err
	}
	return f.Close()
}