<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'

// Rich text for the project description. No image/file nodes: links only.
// The server sanitizes again on save, so this is a convenience, not the security boundary.
const model = defineModel<string>({ default: '' })

const editor = useEditor({
  content: model.value,
  extensions: [StarterKit.configure({ link: { openOnClick: false, protocols: ['http', 'https', 'mailto'] } })],
  editorProps: { attributes: { class: 'rich', 'aria-label': 'รายละเอียดโครงการ' } },
  onUpdate: ({ editor }) => {
    model.value = editor.isEmpty ? '' : editor.getHTML()
  },
})

// external updates (e.g. loading an existing poll)
watch(model, (v) => {
  if (editor.value && v !== (editor.value.isEmpty ? '' : editor.value.getHTML())) editor.value.commands.setContent(v, { emitUpdate: false })
})
onBeforeUnmount(() => editor.value?.destroy())

function setLink() {
  const e = editor.value
  if (!e) return
  const prev = e.getAttributes('link').href as string | undefined
  const url = window.prompt('ลิงก์ (https://…) ใช้ลิงก์ไปยังไฟล์ได้ ระบบไม่รับไฟล์แนบ', prev ?? 'https://')
  if (url === null) return
  if (url.trim() === '') return void e.chain().focus().extendMarkRange('link').unsetLink().run()
  if (!/^(https?:\/\/|mailto:)/i.test(url.trim())) return void window.alert('ลิงก์ต้องขึ้นต้นด้วย http://, https:// หรือ mailto:')
  e.chain().focus().extendMarkRange('link').setLink({ href: url.trim() }).run()
}

const buttons = [
  { label: 'B', title: 'ตัวหนา', cls: 'font-bold', active: 'bold', run: (e: any) => e.chain().focus().toggleBold().run() },
  { label: 'I', title: 'ตัวเอียง', cls: 'italic', active: 'italic', run: (e: any) => e.chain().focus().toggleItalic().run() },
  { label: 'หัวข้อ', title: 'หัวข้อใหญ่', cls: '', active: ['heading', { level: 2 }], run: (e: any) => e.chain().focus().toggleHeading({ level: 2 }).run() },
  { label: 'หัวข้อย่อย', title: 'หัวข้อย่อย', cls: '', active: ['heading', { level: 3 }], run: (e: any) => e.chain().focus().toggleHeading({ level: 3 }).run() },
  { label: '• รายการ', title: 'รายการ', cls: '', active: 'bulletList', run: (e: any) => e.chain().focus().toggleBulletList().run() },
  { label: '1. รายการ', title: 'รายการลำดับเลข', cls: '', active: 'orderedList', run: (e: any) => e.chain().focus().toggleOrderedList().run() },
  { label: '“ อ้างอิง', title: 'อ้างอิง', cls: '', active: 'blockquote', run: (e: any) => e.chain().focus().toggleBlockquote().run() },
]
const isActive = (a: string | [string, object]) => (Array.isArray(a) ? editor.value?.isActive(a[0], a[1]) : editor.value?.isActive(a))
</script>

<template>
  <div class="editor overflow-hidden rounded-lg border border-line bg-surface">
    <div v-if="editor" class="flex flex-wrap gap-1 border-b border-line bg-paper p-1.5" role="toolbar" aria-label="จัดรูปแบบ">
      <button v-for="b in buttons" :key="b.title" type="button" :title="b.title" :aria-pressed="!!isActive(b.active as any)"
        class="cursor-pointer rounded-md px-2.5 py-1 text-sm hover:bg-brand-soft aria-pressed:bg-brand-soft aria-pressed:text-brand" :class="b.cls"
        @click="b.run(editor)">{{ b.label }}</button>
      <button type="button" title="ลิงก์" :aria-pressed="editor.isActive('link')" class="cursor-pointer rounded-md px-2.5 py-1 text-sm hover:bg-brand-soft aria-pressed:bg-brand-soft aria-pressed:text-brand" @click="setLink">ลิงก์</button>
      <span class="mx-1 w-px bg-line"></span>
      <button type="button" title="ย้อนกลับ" class="cursor-pointer rounded-md px-2.5 py-1 text-sm hover:bg-brand-soft disabled:opacity-40" :disabled="!editor.can().undo()" @click="editor.chain().focus().undo().run()">ย้อนกลับ</button>
      <button type="button" title="ทำซ้ำ" class="cursor-pointer rounded-md px-2.5 py-1 text-sm hover:bg-brand-soft disabled:opacity-40" :disabled="!editor.can().redo()" @click="editor.chain().focus().redo().run()">ทำซ้ำ</button>
    </div>
    <EditorContent :editor="editor" class="px-3 py-2 text-[15px]" />
  </div>
</template>
