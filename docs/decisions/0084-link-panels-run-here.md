# 0084: A linked project's panels run on this machine, each an ssh that becomes the host's panel.

This machine's runtime opens, types into and closes each panel, and its view
focuses it. `Remote` answers questions only. The two machines name an agent by a
tag its process carries, so nothing is recorded and no title can move it.

Rejected: one pane attached to a tmux workspace on the host, because tabs,
scrollback, keys and focus were tmux's and every act on an agent was a round
trip.
