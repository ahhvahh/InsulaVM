package vm

// vmDomainName makes the domain name unique for each registered UUIDv7.
// The leading eight hex digits of UUIDv7 are time-derived and cannot safely
// distinguish VMs launched in the same interval.
func vmDomainName(profileName, id string) string {
 return "insula-" + profileName + "-" + id
}
