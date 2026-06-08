#include <linux/module.h>
#include <linux/kernel.h>
#include <linux/string.h>
#include <linux/init.h>
#include <linux/proc_fs.h>
#include <linux/seq_file.h>
#include <linux/mm.h>
#include <linux/sched.h>
#include <linux/timer.h>
#include <linux/jiffies.h>
#include <linux/uaccess.h>
#include <linux/tty.h>
#include <linux/sched/signal.h>
#include <linux/fs.h>
#include <linux/slab.h>
#include <linux/sched/mm.h>
#include <linux/binfmts.h>
#include <linux/timekeeping.h>

MODULE_LICENSE("GPL");
MODULE_AUTHOR("202300733");
MODULE_DESCRIPTION("Modulo de kernel para telemetria de contenedores - SO1");
MODULE_VERSION("1.0");

#define PROC_NAME "continfo_pr1_so1_202300733"
#define MAX_CMDLINE_LENGTH 256

// Función para obtener la línea de comandos de un proceso
static char *get_process_cmdline(struct task_struct *task)
{
    struct mm_struct *mm;
    char *cmdline;
    unsigned long arg_start = 0, arg_end = 0;
    int len = 0, i;

    cmdline = kmalloc(MAX_CMDLINE_LENGTH, GFP_KERNEL);
    if (!cmdline)
        return NULL;

    mm = get_task_mm(task);
    if (!mm) {
        kfree(cmdline);
        return NULL;
    }

    down_read(&mm->mmap_lock);
    arg_start = mm->arg_start;
    arg_end = mm->arg_end;
    up_read(&mm->mmap_lock);

    if (arg_end > arg_start)
        len = arg_end - arg_start;
    else
        len = 0;

    if (len > MAX_CMDLINE_LENGTH - 1)
        len = MAX_CMDLINE_LENGTH - 1;

    if (len > 0) {
        if (access_process_vm(task, arg_start, cmdline, len, 0) != len) {
            mmput(mm);
            kfree(cmdline);
            return NULL;
        }
    } else {
        cmdline[0] = '\0';
    }

    cmdline[len] = '\0';

    for (i = 0; i < len; i++)
        if (cmdline[i] == '\0')
            cmdline[i] = ' ';

    while (len > 0 && cmdline[len - 1] == ' ')
        cmdline[--len] = '\0';

    mmput(mm);
    return cmdline;
}

// Función principal que escribe el JSON en /proc
static int sysinfo_show(struct seq_file *m, void *v)
{
    struct sysinfo si;
    struct task_struct *task;
    unsigned long total_jiffies;
    int first_process = 1;
    int process_count = 0;

    // Métricas de memoria
    si_meminfo(&si);
    total_jiffies = jiffies;

    unsigned long total_kb  = si.totalram  << (PAGE_SHIFT - 10);
    unsigned long free_kb   = si.freeram   << (PAGE_SHIFT - 10);
    unsigned long used_kb   = total_kb - free_kb;

    // Contar procesos
    for_each_process(task) {
        process_count++;
    }

    // Escribir JSON
    seq_printf(m, "{\n");
    seq_printf(m, "  \"Totalram\": %lu,\n", total_kb);
    seq_printf(m, "  \"Freeram\": %lu,\n",  free_kb);
    seq_printf(m, "  \"Usedram\": %lu,\n",  used_kb);
    seq_printf(m, "  \"Procs\": %d,\n", process_count);
    seq_printf(m, "  \"Processes\": [\n");

    for_each_process(task) {
        unsigned long vsz = 0;
        unsigned long rss = 0;
        unsigned long total_kb_inner = si.totalram << (PAGE_SHIFT - 10);
        unsigned long mem_usage = 0;
        unsigned long cpu_usage = 0;
        unsigned long total_time = 0;
        char *cmdline = NULL;

        if (task->mm) {
            vsz = task->mm->total_vm << (PAGE_SHIFT - 10);
            rss = get_mm_rss(task->mm) << (PAGE_SHIFT - 10);

            if (total_kb_inner > 0)
                mem_usage = (rss * 10000) / total_kb_inner;
        }

        total_time = task->utime + task->stime;
        if (total_jiffies > 0) {
            cpu_usage = (total_time * 10000) / total_jiffies;
            cpu_usage = cpu_usage / num_online_cpus();
        }

        cmdline = get_process_cmdline(task);

        if (!first_process)
            seq_printf(m, ",\n");
        else
            first_process = 0;

        seq_printf(m, "    {\n");
        seq_printf(m, "      \"PID\": %d,\n",      task->pid);
        seq_printf(m, "      \"Name\": \"%s\",\n", task->comm);
        // Imprimir cmdline escapando caracteres especiales
        seq_printf(m, "      \"Cmdline\": \"");
        if (cmdline) {
            int ci;
            for (ci = 0; cmdline[ci] != '\0'; ci++) {
                if (cmdline[ci] == '"')
                    seq_printf(m, "\\\"");
                else if (cmdline[ci] == '\\')
                    seq_printf(m, "\\\\");
                else
                    seq_printf(m, "%c", cmdline[ci]);
            }
        } else {
            seq_printf(m, "N/A");
        }
        seq_printf(m, "\",\n");
        seq_printf(m, "      \"VSZ\": %lu,\n",     vsz);
        seq_printf(m, "      \"RSS\": %lu,\n",     rss);
        seq_printf(m, "      \"MemUsage\": \"%lu.%02lu\",\n",
                   mem_usage / 100, mem_usage % 100);
        seq_printf(m, "      \"CPUUsage\": \"%lu.%02lu\"\n",
                   cpu_usage / 100, cpu_usage % 100);
        seq_printf(m, "    }");

        if (cmdline)
            kfree(cmdline);
    }

    seq_printf(m, "\n  ]\n}\n");
    return 0;
}

static int sysinfo_open(struct inode *inode, struct file *file)
{
    return single_open(file, sysinfo_show, NULL);
}

static const struct proc_ops sysinfo_ops = {
    .proc_open    = sysinfo_open,
    .proc_read    = seq_read,
    .proc_lseek   = seq_lseek,
    .proc_release = single_release,
};

static int __init sysinfo_init(void)
{
    proc_create(PROC_NAME, 0, NULL, &sysinfo_ops);
    printk(KERN_INFO "continfo_202300733: modulo cargado\n");
    return 0;
}

static void __exit sysinfo_exit(void)
{
    remove_proc_entry(PROC_NAME, NULL);
    printk(KERN_INFO "continfo_202300733: modulo descargado\n");
}

module_init(sysinfo_init);
module_exit(sysinfo_exit);