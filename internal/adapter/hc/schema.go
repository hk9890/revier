package hc

// The shapes below hold only the fields this host reads. hc's API only grows
// within a protocol generation, so a field it adds is left alone by the
// decode.

type helloResult struct {
	Generation int    `json:"generation"`
	Version    string `json:"version"`
	Core       struct {
		// ID is the core's permanent random id, reset only when its state
		// is wiped: a core restart keeps it.
		ID string `json:"id"`
	} `json:"core"`
	Methods  []string `json:"methods"`
	Features []string `json:"features"`
}

type listResult struct {
	Terminals []terminal `json:"terminals"`
}

// terminal is hc's Terminal record.
type terminal struct {
	// Address is the terminal on this core for good, such as t7; one on
	// another machine is machine/t7.
	Address    string            `json:"address"`
	Name       string            `json:"name"`
	State      string            `json:"state"`
	PID        int               `json:"pid"`
	Argv       []string          `json:"argv"`
	Cwd        string            `json:"cwd"`
	Title      string            `json:"title"`
	Labels     map[string]string `json:"labels"`
	Foreground *foreground       `json:"foreground"`
}

// foreground is what runs in a terminal now: its foreground process group's
// leader, which hc reads about twice a second.
type foreground struct {
	PID  int      `json:"pid"`
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
}
