// Package scheduled keeps a workspace's scheduled tasks: when each one is
// due, running it, and the record of every run. It runs inside the
// workspace, as its account, from one native timer that fires every minute.
package scheduled

import (
	// A workspace without zone files, such as a slim VPS image, still has to
	// read the person's timezone.
	_ "time/tzdata"
)

// Protocol is the version of the JSON the workspace side answers with, so a
// Mac with a newer devmachine notices an older one instead of misreading it.
const Protocol = 1
