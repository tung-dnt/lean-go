// Package paths names every page of the site, so that a link, a form action or a redirect says
// paths.Note(id) and not "/notes/" + id, and a page of one domain can lead to a page of another without
// importing it. It replaces the named routes and Reverse of a router framework.
//
// A path is a constant when the page takes nothing, and a function when it takes the part of the address that
// tells one page from another, which it escapes. A pattern of the router (internal/server/routes.go) and its path
// here are two spellings of one address; internal/server/paths_test.go holds them to each other, so that a path
// that leads nowhere fails a test and not a visitor.
package paths

import "strconv"

// The pages and files that take nothing.
const (
	// Home is the list of notes.
	Home = "/"

	// Notes is where the form of a new note is posted.
	Notes = "/notes"

	// Login is the page of the login form, and where the form is posted.
	Login = "/login"

	// Logout logs the user out.
	Logout = "/logout"

	// Admin is the root of everything that is for the admin: every path at or below it is held to the admin
	// check by the server, whether a route was written for it or not.
	Admin = "/admin"

	// Healthz answers whether the server is ready.
	Healthz = "/healthz"
)

// Where the files are served from. A request below one of these is for a file, and goes through none of the
// middleware that a page does (no session, no user lookup, no cookie).
const (
	// StaticDir is the embedded files of the site's own: its stylesheet, its scripts and its icons. Take the
	// address of one from ui.StaticFile, which adds a version to it.
	StaticDir = "/static/"

	// FilesDir is the files that were put in public/files.
	FilesDir = "/files/"
)

// Note is the page of the note with the id.
func Note(id int64) string {
	return Notes + "/" + strconv.FormatInt(id, 10)
}

// AdminNoteDelete is where the admin posts the deletion of the note with the id.
func AdminNoteDelete(id int64) string {
	return Admin + "/notes/" + strconv.FormatInt(id, 10) + "/delete"
}
