# Tell Planned Mows from Pinned Mows by a date marker in the task description

The app moves its own Scheduled Mows every run, but it must never override a date the owner picked. When the app creates a task, it writes the date it chose into the task's description. A Scheduled Mow is a Planned Mow only if that marker is there and still matches the task's due date. Otherwise it's a Pinned Mow: either the owner created it, or they moved it, and the app leaves it alone. When the app moves a Planned Mow, it rewrites the marker too.

## Considered Options

- **A second label for app-created tasks** (e.g. `lawn-mowing-auto`): rejected because the owner doesn't want to manage labels. It also can't tell when the owner has moved one of the app's tasks, unless they remember to remove the label.

## Consequences

- If the owner edits the description and removes or changes the marker, the task becomes a Pinned Mow. That's an easy, deliberate way to take a task back from the app.
- Changing the marker's format later means old tasks lose their marker and become Pinned Mows, unless the new code still reads the old format.
