package core

import "fmt"

// ActionCommand is what runs for the named action on a project, and the
// directory it runs in. A project on another machine runs the action there,
// through the ssh its remote answers with, in no local directory (decisions.md
// D40). A project on this machine runs run, rendered against the project, in
// its checkout.
func (c *Core) ActionCommand(p Project, name string, run []string) (argv []string, dir string, err error) {
	r, err := c.RemoteOf(p)
	if err != nil {
		return nil, "", fmt.Errorf("action %q: %w", name, err)
	}
	if r != nil {
		return r.RunCommand(p.Remote.Project, name), "", nil
	}
	argv, err = RenderArgv(p.Project, run)
	if err != nil {
		return nil, "", fmt.Errorf("action %q: %w", name, err)
	}
	if len(argv) == 0 {
		return nil, "", fmt.Errorf("action %q runs nothing", name)
	}
	return argv, p.Path, nil
}
