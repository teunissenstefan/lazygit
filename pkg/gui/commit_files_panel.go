package gui

import (
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/jesseduffield/lazygit/pkg/commands/patch"
	"github.com/jesseduffield/lazygit/pkg/gui/filetree"
)

func (gui *Gui) getSelectedCommitFileNode() *filetree.CommitFileNode {
	selectedLine := gui.State.Panels.CommitFiles.SelectedLineIdx
	if selectedLine == -1 || selectedLine > gui.State.CommitFileManager.GetItemsLength()-1 {
		return nil
	}

	return gui.State.CommitFileManager.GetItemAtIndex(selectedLine)
}

func (gui *Gui) getSelectedCommitFile() *models.CommitFile {
	node := gui.getSelectedCommitFileNode()
	if node == nil {
		return nil
	}
	return node.File
}

func (gui *Gui) getSelectedCommitFilePath() string {
	node := gui.getSelectedCommitFileNode()
	if node == nil {
		return ""
	}
	return node.GetPath()
}

func (gui *Gui) handleCommitFileSelect() error {
	gui.escapeLineByLinePanel()

	node := gui.getSelectedCommitFileNode()
	if node == nil {
		return nil
	}

	to := gui.State.CommitFileManager.GetParent()
	from, reverse := gui.getFromAndReverseArgsForDiff(to)

	cmd := gui.OSCommand.ExecutableFromString(
		gui.GitCommand.ShowFileDiffCmdStr(from, to, reverse, node.GetPath(), false),
	)
	task := gui.createRunPtyTask(cmd)

	return gui.refreshMainViews(refreshMainOpts{
		main: &viewUpdateOpts{
			title: "Patch",
			task:  task,
		},
		secondary: gui.secondaryPatchPanelUpdateOpts(),
	})
}

func (gui *Gui) handleCheckoutCommitFile(g *gocui.Gui, v *gocui.View) error {
	node := gui.getSelectedCommitFileNode()
	if node == nil {
		return nil
	}

	if err := gui.GitCommand.CheckoutFile(gui.State.CommitFileManager.GetParent(), node.GetPath()); err != nil {
		return gui.surfaceError(err)
	}

	return gui.refreshSidePanels(refreshOptions{mode: ASYNC})
}

func (gui *Gui) handleDiscardOldFileChange(g *gocui.Gui, v *gocui.View) error {
	if ok, err := gui.validateNormalWorkingTreeState(); !ok {
		return err
	}

	fileName := gui.getSelectedCommitFileName()

	return gui.ask(askOpts{
		title:  gui.Tr.DiscardFileChangesTitle,
		prompt: gui.Tr.DiscardFileChangesPrompt,
		handleConfirm: func() error {
			return gui.WithWaitingStatus(gui.Tr.RebasingStatus, func() error {
				if err := gui.GitCommand.DiscardOldFileChanges(gui.State.Commits, gui.State.Panels.Commits.SelectedLineIdx, fileName); err != nil {
					if err := gui.handleGenericMergeCommandResult(err); err != nil {
						return err
					}
				}

				return gui.refreshSidePanels(refreshOptions{mode: BLOCK_UI})
			})
		},
	})
}

func (gui *Gui) refreshCommitFilesView() error {
	if err := gui.handleRefreshPatchBuildingPanel(-1); err != nil {
		return err
	}

	to := gui.State.Panels.CommitFiles.refName
	from, reverse := gui.getFromAndReverseArgsForDiff(to)

	files, err := gui.GitCommand.GetFilesInDiff(from, to, reverse)
	if err != nil {
		return gui.surfaceError(err)
	}
	gui.State.CommitFileManager.SetFiles(files, to)

	return gui.postRefreshUpdate(gui.Contexts.CommitFiles.Context)
}

func (gui *Gui) handleOpenOldCommitFile(g *gocui.Gui, v *gocui.View) error {
	node := gui.getSelectedCommitFileNode()
	if node == nil {
		return nil
	}

	return gui.openFile(node.GetPath())
}

func (gui *Gui) handleEditCommitFile(g *gocui.Gui, v *gocui.View) error {
	node := gui.getSelectedCommitFileNode()
	if node == nil {
		return nil
	}

	if node.File == nil {
		return gui.createErrorPanel(gui.Tr.ErrCannotEditDirectory)
	}

	return gui.editFile(node.GetPath())
}

func (gui *Gui) handleToggleFileForPatch(g *gocui.Gui, v *gocui.View) error {
	node := gui.getSelectedCommitFileNode()
	if node == nil {
		return nil
	}

	toggleTheFile := func() error {
		if !gui.GitCommand.PatchManager.Active() {
			if err := gui.startPatchManager(); err != nil {
				return err
			}
		}

		// if there is any file that hasn't been fully added we'll fully add everything,
		// otherwise we'll remove everything
		adding := node.AnyFile(func(file *models.CommitFile) bool {
			return gui.GitCommand.PatchManager.GetFileStatus(file.Name, gui.State.CommitFileManager.GetParent()) != patch.WHOLE
		})

		err := node.ForEachFile(func(file *models.CommitFile) error {
			if adding {
				return gui.GitCommand.PatchManager.AddFileWhole(file.Name)
			} else {
				return gui.GitCommand.PatchManager.RemoveFile(file.Name)
			}
		})

		if err != nil {
			return gui.surfaceError(err)
		}

		if gui.GitCommand.PatchManager.IsEmpty() {
			gui.GitCommand.PatchManager.Reset()
		}

		return gui.postRefreshUpdate(gui.Contexts.CommitFiles.Context)
	}

	if gui.GitCommand.PatchManager.Active() && gui.GitCommand.PatchManager.To != gui.State.CommitFileManager.GetParent() {
		return gui.ask(askOpts{
			title:  gui.Tr.DiscardPatch,
			prompt: gui.Tr.DiscardPatchConfirm,
			handleConfirm: func() error {
				gui.GitCommand.PatchManager.Reset()
				return toggleTheFile()
			},
		})
	}

	return toggleTheFile()
}

func (gui *Gui) startPatchManager() error {
	canRebase := gui.State.Panels.CommitFiles.canRebase

	to := gui.State.Panels.CommitFiles.refName
	from, reverse := gui.getFromAndReverseArgsForDiff(to)

	gui.GitCommand.PatchManager.Start(from, to, reverse, canRebase)
	return nil
}

func (gui *Gui) handleEnterCommitFile(g *gocui.Gui, v *gocui.View) error {
	return gui.enterCommitFile(-1)
}

func (gui *Gui) enterCommitFile(selectedLineIdx int) error {
	node := gui.getSelectedCommitFileNode()
	if node == nil {
		return nil
	}

	if node.File == nil {
		return gui.handleToggleCommitFileDirCollapsed()
	}

	enterTheFile := func(selectedLineIdx int) error {
		if !gui.GitCommand.PatchManager.Active() {
			if err := gui.startPatchManager(); err != nil {
				return err
			}
		}

		if err := gui.pushContext(gui.Contexts.PatchBuilding.Context); err != nil {
			return err
		}
		return gui.handleRefreshPatchBuildingPanel(selectedLineIdx)
	}

	if gui.GitCommand.PatchManager.Active() && gui.GitCommand.PatchManager.To != gui.State.CommitFileManager.GetParent() {
		return gui.ask(askOpts{
			title:               gui.Tr.DiscardPatch,
			prompt:              gui.Tr.DiscardPatchConfirm,
			handlersManageFocus: true,
			handleConfirm: func() error {
				gui.GitCommand.PatchManager.Reset()
				return enterTheFile(selectedLineIdx)
			},
			handleClose: func() error {
				return gui.pushContext(gui.Contexts.CommitFiles.Context)
			},
		})
	}

	return enterTheFile(selectedLineIdx)
}

func (gui *Gui) handleToggleCommitFileDirCollapsed() error {
	node := gui.getSelectedCommitFileNode()
	if node == nil {
		return nil
	}

	gui.State.CommitFileManager.ToggleCollapsed(node.GetPath())

	if err := gui.postRefreshUpdate(gui.Contexts.CommitFiles.Context); err != nil {
		gui.Log.Error(err)
	}

	return nil
}

func (gui *Gui) switchToCommitFilesContext(refName string, canRebase bool, context Context, windowName string) error {
	// sometimes the commitFiles view is already shown in another window, so we need to ensure that window
	// no longer considers the commitFiles view as its main view.
	gui.resetWindowForView("commitFiles")

	gui.State.Panels.CommitFiles.SelectedLineIdx = 0
	gui.State.Panels.CommitFiles.refName = refName
	gui.State.Panels.CommitFiles.canRebase = canRebase
	gui.Contexts.CommitFiles.Context.SetParentContext(context)
	gui.Contexts.CommitFiles.Context.SetWindowName(windowName)

	if err := gui.refreshCommitFilesView(); err != nil {
		return err
	}

	return gui.pushContext(gui.Contexts.CommitFiles.Context)
}

// NOTE: this is very similar to handleToggleFileTreeView, could be DRY'd with generics
func (gui *Gui) handleToggleCommitFileTreeView() error {
	path := gui.getSelectedCommitFilePath()

	gui.State.CommitFileManager.ToggleShowTree()

	// find that same node in the new format and move the cursor to it
	if path != "" {
		gui.State.CommitFileManager.ExpandToPath(path)
		index, found := gui.State.CommitFileManager.GetIndexForPath(path)
		if found {
			gui.commitFilesListContext().GetPanelState().SetSelectedLineIdx(index)
		}
	}

	if gui.getCommitFilesView().Context == COMMIT_FILES_CONTEXT_KEY {
		if err := gui.Contexts.CommitFiles.Context.HandleRender(); err != nil {
			return err
		}
	}

	return nil
}
