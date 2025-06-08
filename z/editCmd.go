package z

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

type EditableEntry struct {
	Begin   string `json:"begin"`
	Finish  string `json:"finish"`
	Project string `json:"project"`
	Task    string `json:"task"`
	Notes   string `json:"notes"`
}

var editCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Edit an entry using $EDITOR",
	Long:  "Edit an entry by opening a temporary file in your $EDITOR with the entry data.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		user := GetCurrentUser()
		id := args[0]

		// Get the existing entry
		entry, err := database.GetEntry(user, id)
		if err != nil {
			fmt.Printf("%s %+v\n", CharError, err)
			os.Exit(1)
		}

		// Create editable representation
		editableEntry := EditableEntry{
			Begin:   entry.Begin.Format("2006-01-02 15:04:05 -0700"),
			Project: entry.Project,
			Task:    entry.Task,
			Notes:   entry.Notes,
		}

		// Handle finish time (could be zero for running entries)
		if !entry.Finish.IsZero() {
			editableEntry.Finish = entry.Finish.Format("2006-01-02 15:04:05 -0700")
		}

		// Marshal to JSON
		jsonData, err := json.MarshalIndent(editableEntry, "", "  ")
		if err != nil {
			fmt.Printf("%s Failed to serialize entry: %+v\n", CharError, err)
			os.Exit(1)
		}

		// Create temporary file
		tmpFile, err := ioutil.TempFile("", "zeit-edit-*.json")
		if err != nil {
			fmt.Printf("%s Failed to create temporary file: %+v\n", CharError, err)
			os.Exit(1)
		}
		defer os.Remove(tmpFile.Name())

		// Write JSON to temp file
		if _, err := tmpFile.Write(jsonData); err != nil {
			fmt.Printf("%s Failed to write to temporary file: %+v\n", CharError, err)
			os.Exit(1)
		}
		tmpFile.Close()

		// Get editor from environment
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi" // Default fallback
		}

		// Open editor
		editorCmd := exec.Command(editor, tmpFile.Name())
		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr

		if err := editorCmd.Run(); err != nil {
			fmt.Printf("%s Failed to run editor: %+v\n", CharError, err)
			os.Exit(1)
		}

		// Read modified content
		modifiedData, err := ioutil.ReadFile(tmpFile.Name())
		if err != nil {
			fmt.Printf("%s Failed to read modified file: %+v\n", CharError, err)
			os.Exit(1)
		}

		// Parse modified JSON
		var modifiedEntry EditableEntry
		if err := json.Unmarshal(modifiedData, &modifiedEntry); err != nil {
			fmt.Printf("%s Invalid JSON format: %+v\n", CharError, err)
			os.Exit(1)
		}

		// Validate and update the entry
		if err := validateAndUpdateEntry(user, id, modifiedEntry); err != nil {
			fmt.Printf("%s %+v\n", CharError, err)
			os.Exit(1)
		}

		// Get updated entry and display
		updatedEntry, err := database.GetEntry(user, id)
		if err != nil {
			fmt.Printf("%s Failed to retrieve updated entry: %+v\n", CharError, err)
			os.Exit(1)
		}

		fmt.Printf("%s Entry updated successfully\n", CharInfo)
		fmt.Printf("%s\n", updatedEntry.GetOutput(true))
	},
}

func validateAndUpdateEntry(user string, id string, editableEntry EditableEntry) error {
	// Get the original entry
	originalEntry, err := database.GetEntry(user, id)
	if err != nil {
		return err
	}

	// Create new entry with modified data
	newEntry := originalEntry
	newEntry.Project = editableEntry.Project
	newEntry.Task = editableEntry.Task
	newEntry.Notes = editableEntry.Notes

	// Parse begin time
	if editableEntry.Begin != "" {
		beginTime, err := ParseTime(editableEntry.Begin, time.Time{})
		if err != nil {
			return fmt.Errorf("invalid begin time format: %v", err)
		}
		newEntry.Begin = beginTime
	}

	// Parse finish time (optional)
	if editableEntry.Finish != "" {
		finishTime, err := ParseTime(editableEntry.Finish, time.Time{})
		if err != nil {
			return fmt.Errorf("invalid finish time format: %v", err)
		}
		newEntry.Finish = finishTime
	} else {
		newEntry.Finish = time.Time{} // Reset to zero for running entries
	}

	// Validate time logic
	if !newEntry.IsFinishedAfterBegan() {
		return fmt.Errorf("finish time cannot be before begin time")
	}

	// Check for overlaps with other entries
	if err := checkForOverlaps(user, id, newEntry); err != nil {
		return err
	}

	// Update in database
	_, err = database.UpdateEntry(user, newEntry)
	return err
}

func checkForOverlaps(user string, excludeID string, entry Entry) error {
	// Get all entries for the user
	entries, err := database.ListEntries(user)
	if err != nil {
		return fmt.Errorf("failed to check for overlaps: %v", err)
	}

	entryEnd := entry.Finish
	if entryEnd.IsZero() {
		entryEnd = time.Now() // Use current time for running entries
	}

	for _, existingEntry := range entries {
		// Skip the entry being edited
		if existingEntry.ID == excludeID {
			continue
		}

		existingEnd := existingEntry.Finish
		if existingEnd.IsZero() {
			existingEnd = time.Now() // Use current time for running entries
		}

		// Check for overlap
		if (entry.Begin.Before(existingEnd) && entryEnd.After(existingEntry.Begin)) {
			return fmt.Errorf("entry overlaps with existing entry %s (%s to %s)",
				existingEntry.ID,
				existingEntry.Begin.Format("2006-01-02 15:04:05"),
				existingEnd.Format("2006-01-02 15:04:05"))
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(editCmd)
}