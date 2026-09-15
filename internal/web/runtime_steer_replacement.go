package web

// Session/branch activation rotates the browser instance and retires completed
// run controls. Do this before publishing the replacement instance, including
// failed post-commit verification. Ordinary root completion/admission does not
// call this: late steering receipts retain their original private identity.
func (r *liveRuntime) resetRunControlsForReplacementLocked() {
	r.steer = runtimeSteerState{}
	r.snapshot.Steer = nil
	r.snapshot.SteerACK = nil
	r.compaction = runtimeCompactionState{}
	r.snapshot.Compaction = nil
	r.snapshot.CompactionACK = nil
}
