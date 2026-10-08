package widgets

// canvasFrameProblems checks a frame on Home against what its view can do. A
// view that grows has no least height, only a least width; with size auto the
// frame has no h. owner is how the minimum is named in the message.
func canvasFrameProblems(w Instance, label, path, owner string, sizes []string, view string, grows bool) []Problem {
	var problems []Problem
	at := entryReporter(&problems, w, label, path)
	auto := w.Size == SizeAuto
	if auto && !grows {
		at("size", "%s", noGrowMessage(view, sizes))
		return problems
	}
	if omitsH(w) {
		at("frame", "frame needs h unless size is auto")
	}
	minW, minH := MinFrame(sizes)
	switch {
	case grows:
		if w.Frame.W < minW {
			at("frame", "frame width %d is narrower than %s minimum of %d", w.Frame.W, owner, minW)
		}
		if !auto && w.has("frame.h") && w.Frame.H <= 0 {
			at("frame", "frame h must be above zero")
		}
	case !omitsH(w) && (w.Frame.W < minW || w.Frame.H < minH):
		at("frame", "frame %dx%d is smaller than %s minimum of %dx%d", w.Frame.W, w.Frame.H, owner, minW, minH)
	}
	return problems
}

func omitsH(w Instance) bool {
	return w.Size != SizeAuto && w.Frame.H == 0 && !w.has("frame.h")
}
