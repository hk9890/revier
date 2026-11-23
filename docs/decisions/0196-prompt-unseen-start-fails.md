# 0196: A prompt the agent was not seen to start on fails with its own exit code, though the text arrived.

A script chains a prompt and a wait. A success on an idle agent ran the wait
before a turn that never began. A slow agent gets a false failure, and the text
is in the panel either way, so a script checks the panel before it prompts
again.

Rejected: a longer watch, because it ends in the same choice and holds every
script.
