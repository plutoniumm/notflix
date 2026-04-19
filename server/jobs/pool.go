package jobs

type Pool struct {
	sem chan struct{}
}

func NewPool(n int) *Pool {
	return &Pool{sem: make(chan struct{}, n)}
}

func (p *Pool) Acquire() {
	p.sem <- struct{}{}
}

func (p *Pool) Release() {
	<-p.sem
}

func (p *Pool) Go(fn func()) {
	p.Acquire()

	go func() {
		defer p.Release()
		fn()
	}()
}
