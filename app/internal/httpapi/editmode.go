package httpapi

import "net/http"

// editCookie holds whether someone who may edit a course or its
// downloads sees the editing controls ("1") or the pages as a student does
// (anything else, the default). The header's Edit button sets it
// ("edit-toggle", icons.html); the server still checks every change.
const editCookie = "sb_edit"

// editMode: the viewer asked to see the editing controls.
func editMode(r *http.Request) bool {
	c, err := r.Cookie(editCookie)
	return err == nil && c.Value == "1"
}
