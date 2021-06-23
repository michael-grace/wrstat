/*******************************************************************************
 * Copyright (c) 2021 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/VertebrateResequencing/wr/jobqueue"
	"github.com/spf13/cobra"
	"github.com/wtsi-ssg/wrstat/scheduler"
)

// desiredToJobsMultiplier is how many more jobs we create from a given number
// of desired directories.
const desiredToJobsMultiplier = 2

// options for this cmd.
var workDir string
var finalDir string
var multiJobs int

// multiCmd represents the multi command.
var multiCmd = &cobra.Command{
	Use:   "multi",
	Short: "Get stats on the contents of multiple directories",
	Long: `Get stats on the contents of multiple directories.

wr manager must have been started before running this. If the manager can run
commands on multiple nodes, be sure to set wr's ManagerHost config option to
the host you started the manager on.

This calls 'wrstat walk' and 'wrstat combine' on each of the given directories
of interest. Their outputs go to a unique subdirectory of the given
--working_directory, which means you can start running this before a previous
run has completed on the same inputs, and there won't be conflicts.
It is best if all your directories of interest have different basenames, but
things will still work and not conflict if they don't. To ensure this, the
output directory for each directory of interest is a unique subdirectory of the
unique directory created for all of them.

(When jobs are added to wr's queue to get the work done, they are given a
--rep_grp of wrstat-[cmd]-[directory_basename]-[date]-[unique], so you can use
'wr status -i wrstat -z -o s' to get information on how long everything or
particular subsets of jobs took.)

Once everything has completed, the final output files are moved to the given
--final_output directory, with a name that includes the date this command was
started, the basename of the directory operated on, a unique string per
directory of interest, and a unique string for this call of multi:
[year][month][day]_[directory_basename]/[interest unique].[unique].wrstat.gz
eg. for 'wrstat multi -i foo -w /path/a -f /path/b /mnt/foo /mnt/bar /home/bar'
It might produce: 
/path/b/20210617_foo.clkdnfnd992nfksj1lld.c35m8359bnc8ni7dgphg.wrstat.gz
/path/b/20210617_bar.f8bns3jkd92kds10k4ks.c35m8359bnc8ni7dgphg.wrstat.gz
/path/b/20210617_bar.d498vhsk39fjh129djg8.c35m8359bnc8ni7dgphg.wrstat.gz

Finally, the unique subdirectory of --working_directory that was created is
deleted.`,
	Run: func(cmd *cobra.Command, args []string) {
		if workDir == "" {
			die("--working_directory is required")
		}
		if finalDir == "" {
			die("--final_output is required")
		}
		if len(args) == 0 {
			die("at least 1 directory of interest must be supplied")
		}

		s, d := newScheduler()
		defer d()

		unique := scheduler.UniqueString()
		outputRoot := filepath.Join(workDir, unique)
		err := os.MkdirAll(outputRoot, userOnlyPerm)
		if err != nil {
			die("failed to create working dir: %s", err)
		}

		scheduleWalkJobs(outputRoot, args, unique, multiJobs, s)
		scheduleTidyJob(outputRoot, finalDir, unique, s)
	},
}

func init() {
	RootCmd.AddCommand(multiCmd)

	// flags specific to this sub-command
	multiCmd.Flags().StringVarP(&workDir, "working_directory", "w", "", "base directory for intermediate results")
	multiCmd.Flags().StringVarP(&finalDir, "final_output", "f", "", "final output directory")
	multiCmd.Flags().IntVarP(&multiJobs, "parallel_jobs", "n", 64, "number of parallel stat jobs per walk")
}

// scheduleWalkJobs adds a 'wrstat walk' job to wr's queue for each desired
// path.
func scheduleWalkJobs(outputRoot string, desiredPaths []string, unique string, n int, s *scheduler.Scheduler) {
	jobs := make([]*jobqueue.Job, desiredToJobsMultiplier*len(desiredPaths))

	for i, path := range desiredPaths {
		thisUnique := scheduler.UniqueString()
		outDir := filepath.Join(outputRoot, filepath.Base(path), thisUnique)

		jobs[i*2] = s.NewJob(fmt.Sprintf("%s walk -d %s -o %s -i %s -n %d %s",
			s.Executable(), thisUnique, outDir, statRepGrp(path, unique), n, path),
			walkRepGrp(path, unique), "wrstat-walk", thisUnique, "")

		jobs[i*2+1] = s.NewJob(fmt.Sprintf("%s combine %s", s.Executable(), outDir),
			combineRepGrp(path, unique), "wrstat-stat", unique, thisUnique)
	}

	addJobsToQueue(s, jobs)
}

// walkRepGrp returns a rep_grp that can be used for the walk jobs multi will
// create.
func walkRepGrp(dir, unique string) string {
	return repGrp("walk", dir, unique)
}

// combineRepGrp returns a rep_grp that can be used for the combine jobs multi
// will create.
func combineRepGrp(dir, unique string) string {
	return repGrp("combine", dir, unique)
}

// scheduleTidyJob adds a job to wr's queue that for each working directory
// subdir moves the output to the final location and then deletes the working
// directory.
func scheduleTidyJob(outputRoot, finalDir, unique string, s *scheduler.Scheduler) {
	job := s.NewJob(fmt.Sprintf("%s tidy -f %s -d %s %s", s.Executable(), finalDir, dateStamp(), outputRoot),
		repGrp("tidy", finalDir, unique), "wrstat-tidy", "", unique)

	addJobsToQueue(s, []*jobqueue.Job{job})
}
