package limiter

import (
	"sync"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	logf "github.com/openshift/router/log"
)

var log = logf.Logger.WithName("limiter")

// HandlerFunc defines function signature for a CoalescingSerializingRateLimiter.
type HandlerFunc func() error

// CoalescingSerializingRateLimiter guarantees that calls will not happen to the given function
// more frequently than the given interval, and it guarantees that only one call will happen at a time.
// The calls are not queued, i.e. if you make 5 calls to RegisterChange(), it does not guarantee that the
// handler will be invoked 5 times, it merely guarantees it will be invoked once, and no more often than
// the rate.
// The calls to the handler will happen in the background and are expected to do their own locking if needed.
type CoalescingSerializingRateLimiter struct {
	// handlerFunc is the function to rate limit and seriaize calls to.
	handlerFunc HandlerFunc

	// callInterval is the minimum time between the starts of handler calls.
	callInterval time.Duration

	// lastStart is the time the last run of the handler started.
	lastStart time.Time

	// changeReqTime is nil if no change has been registered since the last handler run completed, otherwise it is the
	// time last change was registered.
	changeReqTime *time.Time

	// handlerRunning indicates whether the Handler is actively running.
	handlerRunning bool

	// stopped indicates no further commits should occur.
	stopped bool

	// lock protects the CoalescingSerializingRateLimiter structure from multiple threads manipulating it at once.
	lock sync.Mutex

	// callbackTimer is the timer we use to make callbacks to re-run the function to decide if we need to do work.
	callbackTimer *time.Timer
}

func NewCoalescingSerializingRateLimiter(interval time.Duration, handlerFunc HandlerFunc) *CoalescingSerializingRateLimiter {
	limiter := &CoalescingSerializingRateLimiter{
		handlerFunc:    handlerFunc,
		callInterval:   interval,
		lastStart:      time.Time{},
		changeReqTime:  nil,
		handlerRunning: false,
	}

	return limiter
}

// Stop signals shutdown and waits until no handler is running. After this method returns
// no handler will be invoked in the future.
func (csrl *CoalescingSerializingRateLimiter) Stop() {
	csrl.lock.Lock()
	csrl.stopped = true
	csrl.lock.Unlock()

	for csrl.isHandlerRunning() {
		time.Sleep(50 * time.Millisecond)
	}
}

func (csrl *CoalescingSerializingRateLimiter) isHandlerRunning() bool {
	csrl.lock.Lock()
	defer csrl.lock.Unlock()
	return csrl.handlerRunning
}

// RegisterChange() indicates that the rate limited function should be called. It may not immediately run it, but it will cause it to run within
// the ReloadInterval.  It will always immediately return, the function will be run in the background.  Not every call to RegisterChange() will
// result in the function getting called.  If it is called repeatedly while it is still within the ReloadInterval since the last run, it will
// only run once when the time allows it.
func (csrl *CoalescingSerializingRateLimiter) RegisterChange() {
	log.V(8).Info("RegisterChange called")
	csrl.changeWorker(true)
}

func (csrl *CoalescingSerializingRateLimiter) changeWorker(userChanged bool) {
	csrl.lock.Lock()
	defer csrl.lock.Unlock()

	log.V(8).Info("changeWorker called")

	if csrl.stopped {
		log.V(8).Info("limiter is stopped")
		return
	}

	if userChanged && csrl.changeReqTime == nil {
		// They just registered a change manually (and we aren't in the middle of a change)
		now := time.Now()
		csrl.changeReqTime = &now
	}

	if csrl.handlerRunning {
		// We don't need to do anything else... there's a run in progress, and when it is done it will re-call this function at which point the work will then happen
		log.V(8).Info("handler was already running, returning from the worker", "running", csrl.handlerRunning, "lastStart", csrl.lastStart.String())
		return
	}

	if csrl.changeReqTime == nil {
		// There's no work queued so we have nothing to do.  We should only get here when
		// the function is re-called after a reload
		log.V(8).Info("no invoke requested time, so there's no queued work.  Nothing to do.")
		return
	}

	// There is no handler running, let's see if we should run yet, or schedule a callback
	now := time.Now()
	sinceLastRun := now.Sub(csrl.lastStart)
	untilNextCallback := csrl.callInterval - sinceLastRun
	log.V(8).Info("checking reload", "now", now, "lastStart", csrl.lastStart, "sinceLast", sinceLastRun, "limit", csrl.callInterval, "remaining", untilNextCallback)

	if untilNextCallback > 0 {
		// We want to reload... but can't yet because some window is not satisfied
		if csrl.callbackTimer == nil {
			csrl.callbackTimer = time.AfterFunc(untilNextCallback, func() { csrl.changeWorker(false) })
		} else {
			// While we are resetting the timer, it should have fired and be stopped.
			// The first time the worker is called it will know the precise duration
			// until when a run would be valid and has scheduled a timer for that point
			csrl.callbackTimer.Reset(untilNextCallback)
		}

		log.V(8).Info("can't invoke the handler yet, need to delay, callback scheduled", "delay", untilNextCallback.String())

		return
	}

	// Otherwise we can reload immediately... let's do it!
	log.V(8).Info("calling the handler function", "invokeTime", csrl.changeReqTime)
	csrl.handlerRunning = true
	csrl.changeReqTime = nil
	csrl.lastStart = now

	// Go run the handler so we don't block the caller
	go csrl.runHandler()

	return
}

func (csrl *CoalescingSerializingRateLimiter) runHandler() {
	// Call the handler, but do it in its own function so we can cleanup in case the handler panics
	runHandler := func() error {
		defer func() {
			csrl.lock.Lock()
			csrl.handlerRunning = false
			csrl.lock.Unlock()
		}()

		return csrl.handlerFunc()
	}
	if err := runHandler(); err != nil {
		utilruntime.HandleError(err)
	}

	// Re-call the commit in case there is work waiting that came in while we were working
	// we want to call the top level commit in case the state has not changed
	log.V(8).Info("re-calling the worker after a reload in case work came in")
	csrl.changeWorker(false)
}
