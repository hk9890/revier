# 0085: A configuration error disables only the smallest thing that is wrong, which stays listed with its reason.

The tool that reaches a broken project must not break with it. A rule refuses
the target it names, a rule about the project refuses the project, and neither
refuses the set.

Rejected: refusing the whole set at load, because one bad file then removes the
surface, the keys and the one place that shows why.
