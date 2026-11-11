package insulavm

// ResolveVMWorkspace returns the per-profile guest destination, falling back
// to the global configuration for old profiles and absent profile metadata.
// It does not require the destination to exist inside the running VM.
func (e *Engine) ResolveVMWorkspace(profileName string)(string,error){
 return e.GuestDestinationForProfile(profileName)
}
