package terminal

import "github.com/omarluq/librecode/internal/assistant"

func (app *App) removeRunningToolBlock(event *assistant.ToolEvent) {
	if event == nil || len(app.runningToolBlocks) == 0 {
		return
	}

	if index, ok := app.runningToolBlockIndexByCallID(event.CallID); ok {
		app.deleteRunningToolBlock(index)

		return
	}

	if index, ok := app.runningToolBlockIndexByArguments(event.Name, event.ArgumentsJSON); ok {
		app.deleteRunningToolBlock(index)

		return
	}

	if index, ok := app.runningToolBlockIndexByName(event.Name); ok {
		app.deleteRunningToolBlock(index)
	}
}

func (app *App) runningToolBlockIndexByCallID(callID string) (int, bool) {
	if callID == "" {
		return 0, false
	}

	for index, block := range app.runningToolBlocks {
		if block.Call.ID == callID {
			return index, true
		}
	}

	return 0, false
}

func (app *App) runningToolBlockIndexByArguments(name, argumentsJSON string) (int, bool) {
	if argumentsJSON == "" {
		return 0, false
	}

	for index, block := range app.runningToolBlocks {
		if block.Call.Name == name && block.Call.ArgumentsJSON == argumentsJSON {
			return index, true
		}
	}

	return 0, false
}

func (app *App) runningToolBlockIndexByName(name string) (int, bool) {
	if name == "" {
		return 0, false
	}

	for index, block := range app.runningToolBlocks {
		if block.Call.Name == name {
			return index, true
		}
	}

	return 0, false
}

func (app *App) deleteRunningToolBlock(index int) {
	if index < 0 || index >= len(app.runningToolBlocks) {
		return
	}

	copy(app.runningToolBlocks[index:], app.runningToolBlocks[index+1:])
	app.runningToolBlocks = app.runningToolBlocks[:len(app.runningToolBlocks)-1]
}
