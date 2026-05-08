package extension

import (
	"context"
	"fmt"
	"sort"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func (manager *Manager) registerTimer(
	extensionRuntime *luaExtension,
	delay time.Duration,
	interval time.Duration,
	function *lua.LFunction,
) uint64 {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	timerID := manager.nextTimerID
	manager.nextTimerID++
	manager.nextHandlerOrder++
	manager.timers = append(manager.timers, luaTimer{
		extension: extensionRuntime,
		function:  function,
		due:       time.Now().Add(max(0, delay)),
		interval:  interval,
		id:        timerID,
		order:     manager.nextHandlerOrder,
	})

	return timerID
}

func (manager *Manager) cancelTimer(timerID uint64) {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	manager.canceledTimers[timerID] = struct{}{}
	manager.timers = filterTimers(manager.timers, func(timer luaTimer) bool {
		return timer.id != timerID
	})
}

func filterTimers(timers []luaTimer, keep func(luaTimer) bool) []luaTimer {
	filtered := timers[:0]
	for _, timer := range timers {
		if keep(timer) {
			filtered = append(filtered, timer)
		}
	}

	return filtered
}

// NextTimerDelay reports the duration until the next scheduled timer is due.
func (manager *Manager) NextTimerDelay(now time.Time) (time.Duration, bool) {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	var nextDue time.Time
	found := false
	for _, timer := range manager.timers {
		if _, canceled := manager.canceledTimers[timer.id]; canceled {
			continue
		}
		if !found || timer.due.Before(nextDue) {
			nextDue = timer.due
			found = true
		}
	}
	if !found {
		return 0, false
	}
	if !nextDue.After(now) {
		return 0, true
	}

	return nextDue.Sub(now), true
}

func (manager *Manager) runDueTimers(ctx context.Context, event *luaHostEvent, now time.Time) error {
	due := manager.takeDueTimers(now)
	for _, timer := range due {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := manager.runTimer(ctx, event, timer); err != nil {
			return err
		}
	}

	return nil
}

func (manager *Manager) takeDueTimers(now time.Time) []luaTimer {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	due := []luaTimer{}
	pending := manager.timers[:0]
	for _, timer := range manager.timers {
		if _, canceled := manager.canceledTimers[timer.id]; canceled {
			delete(manager.canceledTimers, timer.id)
			continue
		}
		if timer.due.After(now) {
			pending = append(pending, timer)
			continue
		}
		due = append(due, timer)
	}
	manager.timers = pending
	sort.SliceStable(due, func(leftIndex, rightIndex int) bool {
		left := due[leftIndex]
		right := due[rightIndex]
		if left.due.Equal(right.due) {
			return left.order < right.order
		}

		return left.due.Before(right.due)
	})

	return due
}

func (manager *Manager) runTimer(ctx context.Context, event *luaHostEvent, timer luaTimer) error {
	result, err := callLuaPrepared(timer.extension, event, timer.function, func(state *lua.LState) []lua.LValue {
		return []lua.LValue{terminalEventTable(state, event.eventSnapshot())}
	})
	if err != nil {
		return fmt.Errorf("extension: timer %d failed: %w", timer.id, err)
	}
	event.applyLuaResult(result)
	if timer.interval > 0 && !manager.timerCanceled(timer.id) {
		manager.rescheduleTimer(timer)
	}

	return ctx.Err()
}

func (manager *Manager) timerCanceled(timerID uint64) bool {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	_, canceled := manager.canceledTimers[timerID]

	return canceled
}

func (manager *Manager) rescheduleTimer(timer luaTimer) {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	if _, canceled := manager.canceledTimers[timer.id]; canceled {
		delete(manager.canceledTimers, timer.id)
		return
	}
	timer.due = time.Now().Add(timer.interval)
	manager.timers = append(manager.timers, timer)
}
