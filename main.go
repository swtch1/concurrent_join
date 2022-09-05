package main

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

func main() {
	s := NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/join", s.Join)
	mux.HandleFunc("/stats", s.Stats)
	httpSrv := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	err := httpSrv.ListenAndServe()
	log.Print(err)
}

// Server is a client connection server.  It informs clients of their
// connection order and keeps track of connection statistics.
type Server struct {
	id     int32
	idLock sync.Mutex

	// timePortal provides an atomic signal between clients to determine their order.
	timePortal chan time.Time
	pq         *PriorityQueue
}

func NewServer() *Server {
	return &Server{
		pq:         NewPriorityQueue(50_000),
		timePortal: make(chan time.Time),
	}
}

func (s *Server) newPairID() int32 {
	s.idLock.Lock()
	defer s.idLock.Unlock()
	s.id++
	return s.id
}

// Stats provides statistics on completed connection pairs.
func (s *Server) Stats(w http.ResponseWriter, r *http.Request) {
	b, err := json.Marshal(s.pq)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintln(w, err)
	}
	fmt.Fprintln(w, string(b))
}

// Join is called by a client when they want to Join the server.  They will be
// paired up, or time out, and notified about their connection order.
func (s *Server) Join(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
	defer cancel()

	ch := make(chan bool)
	go func() {
		isFirst, err := s.register(ctx)
		if err != nil {
			return
		}
		ch <- isFirst
	}()

	select {
	case <-ctx.Done():
		fmt.Fprintln(w, "Timeout: No more connected clients")
	case isFirst := <-ch:
		if isFirst {
			fmt.Fprintln(w, "first")
		} else {
			fmt.Fprintln(w, "second")
		}
	}
}

// register a client.  Return client's registration order.
func (s *Server) register(ctx context.Context) (isFirst bool, _ error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case t := <-s.timePortal:
		// we're second
		p := Pair{
			ID:                      s.newPairID(),
			ConnectDiffMicroseconds: time.Since(t).Microseconds(),
		}
		s.pq.Record(&p)
		return false, nil
	default:
		// continue
	}

	// we're first
	// let's block and wait for another
	now := time.Now()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case s.timePortal <- now: // wow - I just learned selects work for channel sends as well
		return true, nil
	}
}

// Pair identifies a client pair, two distinct but connected clients.
type Pair struct {
	ID int32
	// The time between the start of the first connection and the second
	ConnectDiffMicroseconds int64
	// The index is needed by update and is maintained by the heap.Interface
	// methods. This is the index of the item in the heap.  Do not modify directly.
	index int
}

// PriorityQueue implements heap.Interface.
// Basically stolen from https://pkg.go.dev/container/heap#example-package-PriorityQueue.
type PriorityQueue struct {
	pairs   []*Pair
	maxSize int
}

func NewPriorityQueue(maxSize int) *PriorityQueue {
	return &PriorityQueue{
		maxSize: maxSize,
	}
}

func (pq PriorityQueue) Len() int { return len(pq.pairs) }

func (pq PriorityQueue) Less(i, j int) bool {
	return pq.pairs[i].ConnectDiffMicroseconds < pq.pairs[j].ConnectDiffMicroseconds
}

func (pq PriorityQueue) Swap(i, j int) {
	pq.pairs[i], pq.pairs[j] = pq.pairs[j], pq.pairs[i]
	pq.pairs[i].index = i
	pq.pairs[j].index = j
}

func (pq *PriorityQueue) Push(x any) {
	n := len(pq.pairs)
	item := x.(*Pair)
	item.index = n
	pq.pairs = append(pq.pairs, item)
}

func (pq *PriorityQueue) Pop() any {
	old := pq.pairs
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	pq.pairs = old[0 : n-1]
	return item
}

// Record the pair in the queue.
// Any records over the maximum queue size will be dropped.
func (pq *PriorityQueue) Record(p *Pair) {
	heap.Push(pq, p)
	heap.Fix(pq, p.index)

	// don't let the queue get too large
	for len(pq.pairs) > pq.maxSize {
		_ = pq.Pop()
	}
}

// MarshalJSON builds a custom JSON output for pairs so we can represent them as the spec describes.
func (pq PriorityQueue) MarshalJSON() ([]byte, error) {
	b := strings.Builder{}
	b.WriteString(`{`)
	for _, pair := range pq.pairs {
		b.WriteString(fmt.Sprintf(`"%d": %d,`, pair.ID, pair.ConnectDiffMicroseconds))
	}
	// remove the last comma to keep JSON happy
	s := strings.TrimRight(b.String(), ",")
	s += `}`
	return []byte(s), nil
}
