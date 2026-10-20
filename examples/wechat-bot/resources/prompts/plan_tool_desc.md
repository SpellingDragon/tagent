Delegate to the plan agent: a planning turn that turns a request into a verifiable plan inside the plan workspace, reports the plan's state, and closes it once every task is checked.

Use it when the work needs a verifiable plan before execution, when progress is reported back step by step with evidence, or when an existing plan's status or closure must be settled. The turn runs on that agent's own tools (the plan operation set over the plan backend, and file tools rooted at the plan workspace), and its answer returns as the terminal notification of the admitted task.

Do not use it to execute the planned work, to edit files outside the plan workspace, or to send messages: the plan agent authors and maintains the plan, and this conversation stays the executing side.
