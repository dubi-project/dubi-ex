package cron

func Add(name string, job Job) error {
	return module.Upsert(name, job)
}

func Remove(name string) error {
	return module.Delete(name)
}

func Enable(name string) error {
	return module.Enable(name)
}

func Disable(name string) error {
	return module.Disable(name)
}

func ListJobs() map[string]Job {
	module.mutex.RLock()
	defer module.mutex.RUnlock()

	out := make(map[string]Job, len(module.jobs))
	for name, job := range module.jobs {
		out[name] = cloneJob(job)
	}
	return out
}

func ListLogs(jobName string, offset, limit int) (int64, []Log) {
	return module.ListLogs(jobName, offset, limit)
}
