#include <linux/module.h>
#include <linux/export-internal.h>
#include <linux/compiler.h>

MODULE_INFO(name, KBUILD_MODNAME);

__visible struct module __this_module
__section(".gnu.linkonce.this_module") = {
	.name = KBUILD_MODNAME,
	.init = init_module,
#ifdef CONFIG_MODULE_UNLOAD
	.exit = cleanup_module,
#endif
	.arch = MODULE_ARCH_INIT,
};



static const struct modversion_info ____versions[]
__used __section("__versions") = {
	{ 0xce105414, "single_open" },
	{ 0xbd03ed67, "__ref_stack_chk_guard" },
	{ 0xc7ffe1aa, "si_meminfo" },
	{ 0x058c185a, "jiffies" },
	{ 0x5f41ea90, "init_task" },
	{ 0x0d8b6c91, "seq_printf" },
	{ 0x2182515b, "__num_online_cpus" },
	{ 0xbd03ed67, "random_kmalloc_seed" },
	{ 0x08bfc903, "kmalloc_caches" },
	{ 0xecd17989, "__kmalloc_cache_noprof" },
	{ 0xdbd50071, "get_task_mm" },
	{ 0xa59da3c0, "down_read" },
	{ 0xa59da3c0, "up_read" },
	{ 0x40ae3ae0, "mmput" },
	{ 0xcb8b6ec6, "kfree" },
	{ 0x4f2ea465, "access_process_vm" },
	{ 0xd272d446, "__stack_chk_fail" },
	{ 0xcdeafffc, "remove_proc_entry" },
	{ 0x11bacf83, "seq_read" },
	{ 0xd5bc7086, "seq_lseek" },
	{ 0x024d45a2, "single_release" },
	{ 0xd272d446, "__fentry__" },
	{ 0x92878b20, "proc_create" },
	{ 0xe8213e80, "_printk" },
	{ 0xd272d446, "__x86_return_thunk" },
	{ 0x814e12e5, "module_layout" },
};

static const u32 ____version_ext_crcs[]
__used __section("__version_ext_crcs") = {
	0xce105414,
	0xbd03ed67,
	0xc7ffe1aa,
	0x058c185a,
	0x5f41ea90,
	0x0d8b6c91,
	0x2182515b,
	0xbd03ed67,
	0x08bfc903,
	0xecd17989,
	0xdbd50071,
	0xa59da3c0,
	0xa59da3c0,
	0x40ae3ae0,
	0xcb8b6ec6,
	0x4f2ea465,
	0xd272d446,
	0xcdeafffc,
	0x11bacf83,
	0xd5bc7086,
	0x024d45a2,
	0xd272d446,
	0x92878b20,
	0xe8213e80,
	0xd272d446,
	0x814e12e5,
};
static const char ____version_ext_names[]
__used __section("__version_ext_names") =
	"single_open\0"
	"__ref_stack_chk_guard\0"
	"si_meminfo\0"
	"jiffies\0"
	"init_task\0"
	"seq_printf\0"
	"__num_online_cpus\0"
	"random_kmalloc_seed\0"
	"kmalloc_caches\0"
	"__kmalloc_cache_noprof\0"
	"get_task_mm\0"
	"down_read\0"
	"up_read\0"
	"mmput\0"
	"kfree\0"
	"access_process_vm\0"
	"__stack_chk_fail\0"
	"remove_proc_entry\0"
	"seq_read\0"
	"seq_lseek\0"
	"single_release\0"
	"__fentry__\0"
	"proc_create\0"
	"_printk\0"
	"__x86_return_thunk\0"
	"module_layout\0"
;

MODULE_INFO(depends, "");


MODULE_INFO(srcversion, "68E58432C4EE8AFEC39B8AA");
