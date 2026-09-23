# LawnMowingPredictor

Keeps lawn mowing on the owner's radar by putting a mowing task in Todoist on a good day, based on when the lawn was last mowed and the rain forecast. It's a reminder, not an autopilot: long runs of bad weather are left to the owner.

## Language

### Mowing history

**Mow**:
A completed Todoist task carrying the mowing label. A ticked task is the definition of a mowed lawn: nothing else counts, and a ticked task counts even if no mowing happened.
_Avoid_: Mowing event, cut

**Mow Date**:
The local calendar date (US Eastern) on which a **Mow**'s task was ticked off, not the date it was due.
_Avoid_: Due date (for a Mow), completion time

**Last Mow**:
The most recent **Mow**. When there isn't one, mowing can happen as soon as a **Good Mowing Day** comes along.

### Scheduling

**Scheduled Mow**:
Any open Todoist task carrying the mowing label, whatever its due date (overdue tasks count). There is at most one at a time.
_Avoid_: Pending mow, upcoming task

**Planned Mow**:
A **Scheduled Mow** the app created and still controls. Its due date is still the date the app chose, so the app may move it.

**Pinned Mow**:
A **Scheduled Mow** the owner controls, either because they created it or because they moved a **Planned Mow** to a different day. The app never changes it.
_Avoid_: Manual mow, locked task

**Minimum Interval**:
The fewest days after the **Last Mow** that a new mow may happen. Mowing sooner would be wasted effort.

**Ideal Interval**:
The number of days after the **Last Mow** that the owner would like to mow again.

**Target Date**:
**Last Mow** + **Ideal Interval**: the day the next mow would happen if the weather allowed.
_Avoid_: Due date, eligible date

**Good Mowing Day**:
A day dry enough to mow: the chance of rain is at or under the threshold on the day itself and on the day before, so the grass has dried out. When the day is today, the day before counts as dry.
_Avoid_: Dry day, safe day

**Mow Day**:
The day chosen for the next mow: the **Target Date** if it's a **Good Mowing Day**; otherwise the nearest good day before it, going back no further than the **Minimum Interval**; otherwise the first good day after it. When the forecast shows no good day (or doesn't reach that far yet), it's the **Target Date**, or today if the Target Date has passed.
_Avoid_: Chosen date, eligible date
