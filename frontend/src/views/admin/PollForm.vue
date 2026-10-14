<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, type Kind, type Poll, type PollInput } from '../../api'
import { fromLocalInput, kindLabel, toLocalInput } from '../../format'
import RichEditor from '../../components/RichEditor.vue'

const props = defineProps<{ id?: string }>()
const router = useRouter()

const now = new Date()
now.setMinutes(0, 0, 0)
const form = ref({
  title: '',
  description: '',
  kind: 'approve' as Kind,
  max_choices: 2,
  anonymous: true,
  results_visibility: 'after_close' as 'live' | 'after_close',
  starts_at: toLocalInput(new Date(now.getTime() + 3600_000).toISOString()),
  ends_at: toLocalInput(new Date(now.getTime() + 7 * 86400_000).toISOString()),
  allow_comment: false,
  questions: [''],
  options: ['', ''],
})
const loading = ref(!!props.id)
const busy = ref(false)
const error = ref('')
const locked = ref(false)

// promo image: optional; uploaded after the poll itself is saved
const MAX_IMAGE = 2 * 1024 * 1024
const imageFile = ref<File | null>(null)
const imageUrl = ref('') // what the form currently shows (existing or newly picked)
const hadImage = ref(false)
const imageError = ref('')

function pickImage(ev: Event) {
  const f = (ev.target as HTMLInputElement).files?.[0]
  imageError.value = ''
  if (!f) return
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(f.type)) imageError.value = 'ใช้ไฟล์ JPG, PNG หรือ WebP เท่านั้น'
  else if (f.size > MAX_IMAGE) imageError.value = 'ไฟล์ใหญ่เกิน 2 MB'
  else {
    if (imageUrl.value.startsWith('blob:')) URL.revokeObjectURL(imageUrl.value)
    imageFile.value = f
    imageUrl.value = URL.createObjectURL(f)
  }
  ;(ev.target as HTMLInputElement).value = ''
}
function clearImage() {
  if (imageUrl.value.startsWith('blob:')) URL.revokeObjectURL(imageUrl.value)
  imageFile.value = null
  imageUrl.value = ''
}

onMounted(async () => {
  if (!props.id) return
  try {
    const p = await api.get<Poll>(`/admin/polls/${props.id}`)
    locked.value = p.status !== 'draft' && p.status !== 'upcoming'
    form.value = {
      title: p.title,
      description: p.description,
      kind: p.kind,
      max_choices: p.max_choices,
      anonymous: p.anonymous,
      results_visibility: p.results_visibility,
      starts_at: toLocalInput(p.starts_at),
      ends_at: toLocalInput(p.ends_at),
      allow_comment: p.allow_comment,
      questions: p.kind === 'approve' && p.questions.length ? p.questions.map((q) => q.label) : [''],
      options: p.kind === 'approve' || p.kind === 'comment' ? ['', ''] : p.options.map((o) => o.label),
    }
    imageUrl.value = p.image_url
    hadImage.value = !!p.image_url
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
})

const kinds: Kind[] = ['single', 'multi', 'approve', 'comment']
const needsOptions = () => form.value.kind === 'single' || form.value.kind === 'multi'

async function save() {
  if (imageError.value) return
  busy.value = true
  error.value = ''
  const f = form.value
  const body: PollInput = {
    title: f.title,
    description: f.description,
    kind: f.kind,
    max_choices: f.kind === 'multi' ? Math.min(f.max_choices, f.options.filter((o) => o.trim()).length) : 1,
    anonymous: f.anonymous,
    results_visibility: f.results_visibility,
    starts_at: fromLocalInput(f.starts_at),
    ends_at: fromLocalInput(f.ends_at),
    allow_comment: f.kind !== 'comment' && f.allow_comment,
    questions: f.kind === 'approve' ? f.questions.map((q) => q.trim()).filter(Boolean) : [],
    options: needsOptions() ? f.options.map((o) => o.trim()).filter(Boolean) : [],
  }
  try {
    const r = props.id
      ? await api.put<{ id: string }>(`/admin/polls/${props.id}`, body)
      : await api.post<{ id: string }>('/admin/polls', body)
    if (imageFile.value) await api.upload(`/admin/polls/${r.id}/image`, imageFile.value)
    else if (hadImage.value && !imageUrl.value) await api.del(`/admin/polls/${r.id}/image`)
    router.push(`/admin/polls/${r.id}`)
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-2xl">
    <h1 class="text-2xl font-bold">{{ id ? 'แก้ไขแบบโหวต' : 'สร้างแบบโหวตใหม่' }}</h1>
    <p v-if="loading" class="py-12 text-center text-muted">กำลังโหลด…</p>
    <p v-else-if="locked" class="error-box mt-6">แบบโหวตนี้เริ่มแล้ว จึงแก้ไขไม่ได้ เพื่อรักษาความน่าเชื่อถือของผลโหวต</p>

    <form v-else class="mt-6 space-y-6" @submit.prevent="save">
      <section class="panel space-y-4 p-5 sm:p-6">
        <div>
          <label class="label" for="t">หัวข้อ</label>
          <input id="t" v-model="form.title" class="field" maxlength="200" required />
        </div>
        <div>
          <span class="label">รายละเอียดโครงการ</span>
          <RichEditor v-model="form.description" />
          <p class="hint">ตัวหนา หัวข้อ รายการ และลิงก์ได้ ระบบไม่รับไฟล์แนบ หากมีเอกสารให้แปะลิงก์ไปยังไฟล์</p>
        </div>
        <div>
          <span class="label">รูปโปรโมต (ไม่บังคับ)</span>
          <div v-if="imageUrl" class="mb-2 flex items-start gap-3">
            <img :src="imageUrl" alt="ตัวอย่างรูปโปรโมต" class="max-h-40 rounded-lg border border-line object-cover" />
            <button type="button" class="btn btn-ghost" @click="clearImage">ลบรูป</button>
          </div>
          <input type="file" accept="image/jpeg,image/png,image/webp" class="field" aria-label="เลือกรูปโปรโมต" @change="pickImage" />
          <p class="hint">JPG, PNG หรือ WebP ไม่เกิน 2 MB</p>
          <p v-if="imageError" class="error-box mt-2" role="alert">{{ imageError }}</p>
        </div>
      </section>

      <section class="panel space-y-4 p-5 sm:p-6">
        <div>
          <span class="label">รูปแบบ</span>
          <div class="grid gap-2 sm:grid-cols-2">
            <label v-for="k in kinds" :key="k" class="flex cursor-pointer items-center gap-2 rounded-lg border border-line px-3 py-2.5 has-[:checked]:border-brand has-[:checked]:bg-brand-soft">
              <input v-model="form.kind" type="radio" :value="k" class="accent-[var(--color-brand)]" /> {{ kindLabel[k] }}
            </label>
          </div>
          <p v-if="form.kind === 'approve'" class="hint">ตัวเลือกคือ เห็นด้วย / ไม่เห็นด้วย / งดออกเสียง ในแต่ละประเด็น</p>
          <p v-if="form.kind === 'comment'" class="hint">ผู้ใช้พิมพ์ความเห็นได้ 1 ครั้ง (ไม่เกิน 2,000 ตัวอักษร)</p>
        </div>

        <div v-if="form.kind === 'approve'">
          <span class="label">ประเด็นที่ขอความเห็น (ไม่เกิน 50 ข้อ)</span>
          <div class="space-y-2">
            <div v-for="(_, i) in form.questions" :key="i" class="flex gap-2">
              <input v-model="form.questions[i]" class="field" maxlength="500" :aria-label="`ประเด็นที่ ${i + 1}`" :placeholder="`ประเด็นที่ ${i + 1} เช่น มาตรา ${i + 1}`" />
              <button type="button" class="btn btn-ghost !px-3" :disabled="form.questions.length <= 1" aria-label="ลบประเด็น" @click="form.questions.splice(i, 1)">ลบ</button>
            </div>
          </div>
          <button type="button" class="btn btn-ghost mt-2" :disabled="form.questions.length >= 50" @click="form.questions.push('')">เพิ่มประเด็น</button>
          <p class="hint">ถ้ามีประเด็นเดียว เว้นว่างได้ ใช้หัวข้อของแบบโหวตเป็นคำถาม ผู้โหวตต้องตอบทุกประเด็น และระบบสรุปผลตามเสียงข้างมาก (ไม่นับงดออกเสียง)</p>
        </div>

        <label v-if="form.kind !== 'comment'" class="flex cursor-pointer items-start gap-3 rounded-lg border border-line p-3 has-[:checked]:border-brand has-[:checked]:bg-brand-soft">
          <input v-model="form.allow_comment" type="checkbox" class="mt-1.5 accent-[var(--color-brand)]" />
          <span><b>เปิดช่องความเห็นปิดท้าย</b><br /><span class="text-sm text-muted">ผู้โหวตพิมพ์ความเห็นเพิ่มเติมได้ ไม่บังคับ (ไม่เกิน 2,000 ตัวอักษร)</span></span>
        </label>

        <div v-if="needsOptions()">
          <span class="label">ตัวเลือก (2–20 ข้อ)</span>
          <div class="space-y-2">
            <div v-for="(_, i) in form.options" :key="i" class="flex gap-2">
              <input v-model="form.options[i]" class="field" maxlength="200" :aria-label="`ตัวเลือกที่ ${i + 1}`" :placeholder="`ตัวเลือกที่ ${i + 1}`" />
              <button type="button" class="btn btn-ghost !px-3" :disabled="form.options.length <= 2" aria-label="ลบตัวเลือก" @click="form.options.splice(i, 1)">ลบ</button>
            </div>
          </div>
          <button type="button" class="btn btn-ghost mt-2" :disabled="form.options.length >= 20" @click="form.options.push('')">เพิ่มตัวเลือก</button>
        </div>

        <div v-if="form.kind === 'multi'">
          <label class="label" for="m">เลือกได้สูงสุด</label>
          <input id="m" v-model.number="form.max_choices" type="number" min="1" :max="form.options.length" class="field !w-28" />
        </div>
      </section>

      <section class="panel space-y-5 p-5 sm:p-6">
        <div>
          <span class="label">การระบุตัวตนผู้โหวต</span>
          <label class="mb-2 flex cursor-pointer gap-3 rounded-lg border border-line p-3 has-[:checked]:border-brand has-[:checked]:bg-brand-soft">
            <input v-model="form.anonymous" type="radio" :value="true" class="mt-1.5 accent-[var(--color-brand)]" />
            <span><b>ไม่ระบุตัวตน</b><br /><span class="text-sm text-muted">ระบบเก็บว่าใครโหวตแล้ว แต่ไม่เชื่อมกับตัวเลือกที่เลือก</span></span>
          </label>
          <label class="flex cursor-pointer gap-3 rounded-lg border border-line p-3 has-[:checked]:border-brand has-[:checked]:bg-brand-soft">
            <input v-model="form.anonymous" type="radio" :value="false" class="mt-1.5 accent-[var(--color-brand)]" />
            <span><b>เปิดเผยชื่อ</b><br /><span class="text-sm text-muted">ทุกคนเห็นว่าใครเลือกอะไร เหมาะกับการลงมติแบบเปิด</span></span>
          </label>
        </div>
        <div>
          <span class="label">การแสดงผล</span>
          <label class="mb-2 flex cursor-pointer gap-3 rounded-lg border border-line p-3 has-[:checked]:border-brand has-[:checked]:bg-brand-soft">
            <input v-model="form.results_visibility" type="radio" value="after_close" class="mt-1.5 accent-[var(--color-brand)]" />
            <span><b>แสดงเมื่อปิดโหวต</b><br /><span class="text-sm text-muted">ป้องกันผลที่เห็นอยู่ชักนำผู้ที่ยังไม่โหวต</span></span>
          </label>
          <label class="flex cursor-pointer gap-3 rounded-lg border border-line p-3 has-[:checked]:border-brand has-[:checked]:bg-brand-soft">
            <input v-model="form.results_visibility" type="radio" value="live" class="mt-1.5 accent-[var(--color-brand)]" />
            <span><b>แสดงสด</b><br /><span class="text-sm text-muted">ทุกคนเห็นผลทันทีที่มีการโหวต</span></span>
          </label>
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="label" for="s">เริ่มโหวต</label>
            <input id="s" v-model="form.starts_at" type="datetime-local" class="field" required />
          </div>
          <div>
            <label class="label" for="e">สิ้นสุด</label>
            <input id="e" v-model="form.ends_at" type="datetime-local" class="field" required />
          </div>
          <p class="hint sm:col-span-2 !mt-0">เวลาตามเขตเวลาของเครื่องที่คุณใช้อยู่ หลังเริ่มโหวตแล้วจะแก้ไขไม่ได้</p>
        </div>
      </section>

      <p v-if="error" class="error-box" role="alert">{{ error }}</p>
      <div class="flex gap-3">
        <button class="btn btn-primary" :disabled="busy">{{ busy ? 'กำลังบันทึก…' : 'บันทึกแบบร่าง' }}</button>
        <RouterLink :to="id ? `/admin/polls/${id}` : '/admin'" class="btn btn-ghost no-underline">ยกเลิก</RouterLink>
      </div>
    </form>
  </div>
</template>
