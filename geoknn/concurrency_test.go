package geoknn

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

// 查询与导入、删除、替换并发交错：每次查询都必须看到某一时刻的完整数据。
// 用 -race 运行以检测数据竞争。
func TestConcurrentQueryAndEdit(t *testing.T) {
	e := NewEngine()
	// 初始数据
	for b := 0; b < 4; b++ {
		batch := make([]Feature, 0, 500)
		for i := 0; i < 500; i++ {
			batch = append(batch, Feature{
				ID:    fmt.Sprintf("init-%d-%d", b, i),
				Layer: fmt.Sprintf("L%d", i%5),
				WKT:   fmt.Sprintf("POINT(%f %f)", -180+360.0*float64(i)/500, -80+160.0*float64(b)/4),
			})
		}
		if err := e.Import(batch); err != nil {
			t.Fatal(err)
		}
	}

	var readers, writers sync.WaitGroup
	stop := make(chan struct{})
	errCh := make(chan error, 64)

	// 8 个查询 goroutine
	for w := 0; w < 8; w++ {
		readers.Add(1)
		go func(seed int64) {
			defer readers.Done()
			rng := rand.New(rand.NewSource(seed))
			for {
				select {
				case <-stop:
					return
				default:
				}
				q := Query{
					Lon: -180 + 360*rng.Float64(),
					Lat: -90 + 180*rng.Float64(),
					K:   1 + rng.Intn(100),
				}
				if rng.Intn(2) == 0 {
					q.Layers = []string{fmt.Sprintf("L%d", rng.Intn(6))}
				}
				got, err := e.Query(q)
				if err != nil {
					errCh <- err
					return
				}
				for i := 1; i < len(got); i++ {
					if got[i].Distance < got[i-1].Distance {
						errCh <- fmt.Errorf("结果未排序: %v", got)
						return
					}
				}
			}
		}(int64(w))
	}

	// 1 个写 goroutine：导入、替换、删除交错
	writers.Add(1)
	go func() {
		defer writers.Done()
		for round := 0; round < 50; round++ {
			batch := make([]Feature, 0, 100)
			for i := 0; i < 100; i++ {
				batch = append(batch, Feature{
					ID:    fmt.Sprintf("w-%d-%d", round, i),
					Layer: fmt.Sprintf("L%d", i%5),
					WKT:   fmt.Sprintf("POINT(%f %f)", float64(i)-50, float64(round)-25),
				})
			}
			if err := e.Import(batch); err != nil {
				errCh <- err
				return
			}
			if err := e.ReplaceGeometry(fmt.Sprintf("w-%d-0", round), "LINESTRING(0 0, 1 1)"); err != nil {
				errCh <- err
				return
			}
			if err := e.Delete(fmt.Sprintf("w-%d-99", round)); err != nil {
				errCh <- err
				return
			}
		}
	}()

	writers.Wait()
	close(stop)
	readers.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}
