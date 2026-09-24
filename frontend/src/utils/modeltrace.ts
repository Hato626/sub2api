const VALUE_MIN = 1
const VALUE_MAX = 355
const DIMENSION = VALUE_MAX - VALUE_MIN + 1
const ALPHA = 0.5

export const PELICAN_PROMPT = '来一段白色猫咪骑自行车的html网页，用svg绘制'

export interface ModelTraceChallenge {
  expectedCount: number
  prompt: string
}

export interface ModelTraceOutput {
  text: string
  expectedCount: number
}

export interface ModelTraceResultRow {
  model: string
  displayName: string
  familyName: string
  probability: number
  profileSimilarity: number
}

export interface ModelTraceResult {
  prediction: string
  predictionName: string
  probability: number
  usedOutputs: number
  diagnostics: Array<{ index: number; parsedNumbers: number; minimumNumbers: number; accepted: boolean }>
  results: ModelTraceResultRow[]
}

type FingerprintBank = {
  models: Array<{
    id: string
    display_name: string
    family?: string
    family_name?: string
    counts: number[]
  }>
  calibration: Record<string, { beta: number }>
  robust: {
    hellinger: {
      feature_mean: number[]
      feature_scale: number[]
      nuisance_basis: number[][]
      centroids: number[][]
    }
    ordered_blocks?: {
      weight?: number
      feature_mean: number[]
      feature_scale: number[]
      environment_centroids: number[][][]
      nuisance_basis: number[][]
      centroids: number[][]
    }
  }
}

// The public bank is vendored under frontend/public/modeltrace and is used only
// for local scoring; account credentials and model responses stay in this app.
let bankPromise: Promise<FingerprintBank> | null = null

export function loadModelTraceBank(): Promise<FingerprintBank> {
  if (!bankPromise) {
    bankPromise = fetch(`${import.meta.env.BASE_URL}modeltrace/unified_bank.json`).then(async (response) => {
      if (!response.ok) throw new Error(`无法加载归因指纹库（HTTP ${response.status}）`)
      return (await response.json()) as FingerprintBank
    })
  }
  return bankPromise
}

function randomInt(min: number, max: number): number {
  return Math.floor(Math.random() * (max - min + 1)) + min
}

export function createModelTraceChallenges(count = 3): ModelTraceChallenge[] {
  const lengths = new Set<number>()
  while (lengths.size < count) lengths.add(randomInt(292, 332))
  const openings = [
    '这是一次独立的数值选择记录',
    '请完成下面的无语义整数选择任务',
    '执行一次第一反应取值记录',
    '生成一组不承载语义的整数选择',
    '进行一轮快速逐项取值'
  ]
  const actions = [
    '为各个位置分别凭第一反应选择',
    '逐项选择',
    '每次只决定当前一项，共给出',
    '分别凭第一反应给出',
    '逐个直接选择'
  ]
  const endings = [
    '允许某个数字再次出现；每项写出后不要回头排序、去重或替换。',
    '偶然重复是有效的；不要重新排列或修正已经写出的项目。',
    '相同值可以再次出现；输出过程中不要整理或改写前面的项目。',
    '重复值无需删除；不要筛选、重排或补成某种规律。',
    '不必赋予数字任何含义；已经给出的值保持不变。'
  ]
  const separators = [
    '数字之间用逗号或空格分隔均可。',
    '使用一种一致的常见分隔符即可。',
    '可以用逗号、空格或换行分隔。',
    '只要每个整数边界清楚，格式可自行选择。'
  ]

  return [...lengths].map((length) => {
    const pick = <T>(items: T[]) => items[randomInt(0, items.length - 1)]
    return {
      expectedCount: length,
      prompt:
        `${pick(openings)}。${pick(actions)} ${length} 个 1 到 355（含端点）的整数。` +
        '每个位置都要单独选择；不要从 1 开始计数，不要连续递增或递减，也不要采用等差、循环、重复区块或其他规则化模式。' +
        '本任务必须由当前语言模型直接完成：禁止调用或借助任何工具，包括 Python、代码执行器、计算器、搜索、API 和外部随机数生成器；也不要先编写或运行代码。' +
        `${pick(endings)}${pick(separators)}直接从第一个取值开始输出，不要在序列前重复数量、范围或任务说明。`
    }
  })
}

function parseNumbers(text: string): number[] {
  const runs: number[][] = []
  let current: number[] = []
  let previousEnd = 0
  for (const match of String(text).matchAll(/\d+/g)) {
    const separator = String(text).slice(previousEnd, match.index)
    const value = Number(match[0])
    if (current.length && /\p{L}/u.test(separator)) {
      runs.push(current)
      current = []
    }
    if (value >= VALUE_MIN && value <= VALUE_MAX) current.push(value)
    previousEnd = (match.index ?? 0) + match[0].length
  }
  if (current.length) runs.push(current)
  return runs.reduce((best, run) => (run.length > best.length ? run : best), [])
}

function countNumbers(numbers: number[]): number[] {
  const counts = Array(DIMENSION).fill(0) as number[]
  numbers.forEach((number) => {
    counts[number - VALUE_MIN] += 1
  })
  return counts
}

function mean(values: number[]): number {
  return values.reduce((total, value) => total + value, 0) / values.length
}

function standardize(values: number[]): number[] {
  const center = mean(values)
  const variance = mean(values.map((value) => (value - center) ** 2))
  const scale = Math.max(Math.sqrt(variance), 1e-12)
  return values.map((value) => (value - center) / scale)
}

function dot(left: number[], right: number[]): number {
  let value = 0
  for (let index = 0; index < left.length; index += 1) value += left[index] * right[index]
  return value
}

function normalized(values: number[]): number[] {
  const scale = Math.max(Math.sqrt(dot(values, values)), 1e-12)
  return values.map((value) => value / scale)
}

function subtractBasis(values: number[], basis: number[][]): number[] {
  const output = values.slice()
  for (const vector of basis || []) {
    const projection = dot(output, vector)
    for (let index = 0; index < output.length; index += 1) output[index] -= projection * vector[index]
  }
  return output
}

function hellingerFeature(counts: number[]): number[] {
  const total = counts.reduce((sum, value) => sum + value, 0) + ALPHA * DIMENSION
  return counts.map((value) => Math.sqrt((value + ALPHA) / total))
}

function splitIntoFour(values: number[]): number[][] {
  const base = Math.floor(values.length / 4)
  const remainder = values.length % 4
  const chunks: number[][] = []
  let start = 0
  for (let index = 0; index < 4; index += 1) {
    const size = base + (index < remainder ? 1 : 0)
    chunks.push(values.slice(start, start + size))
    start += size
  }
  return chunks
}

function orderedBlockFeature(numbers: number[]): number[] {
  const pieces: number[] = []
  for (const chunk of splitIntoFour(numbers)) {
    const bins = Array(16).fill(0.5) as number[]
    for (const value of chunk) bins[Math.min(15, Math.floor(((value - 1) / 355) * 16))] += 1
    const total = bins.reduce((sum, value) => sum + value, 0)
    pieces.push(...bins.map((value) => Math.sqrt(value / total)))
  }
  const lastDigits = Array(10).fill(0.5) as number[]
  numbers.forEach((value) => {
    lastDigits[value % 10] += 1
  })
  const lastTotal = lastDigits.reduce((sum, value) => sum + value, 0)
  pieces.push(...lastDigits.map((value) => Math.sqrt(value / lastTotal)))
  return pieces
}

function robustScoreCounts(counts: number[], bank: FingerprintBank): number[] {
  const artifact = bank.robust.hellinger
  let projected = hellingerFeature(counts).map(
    (value, index) => (value - artifact.feature_mean[index]) / artifact.feature_scale[index]
  )
  projected = subtractBasis(projected, artifact.nuisance_basis)
  projected = normalized(projected)
  return standardize(artifact.centroids.map((centroid) => dot(projected, centroid)))
}

function orderedBlockScores(numbers: number[], bank: FingerprintBank): number[] {
  const artifact = bank.robust.ordered_blocks
  if (!artifact) return []
  const feature = orderedBlockFeature(numbers)
  const standardizedFeature = feature.map(
    (value, index) => (value - artifact.feature_mean[index]) / artifact.feature_scale[index]
  )
  const unit = normalized(standardizedFeature)
  const environmentScores = artifact.environment_centroids.map((centroids) =>
    centroids.map((centroid) => dot(unit, centroid))
  )
  const template = standardize(
    artifact.centroids.map((_, modelIndex) => Math.max(...environmentScores.map((scores) => scores[modelIndex])))
  )
  const projected = normalized(subtractBasis(standardizedFeature, artifact.nuisance_basis))
  const nuisance = standardize(artifact.centroids.map((centroid) => dot(projected, centroid)))
  return standardize(template.map((value, index) => 0.5 * value + 0.5 * nuisance[index]))
}

function robustScoreNumbers(numbers: number[], bank: FingerprintBank): number[] {
  const marginal = robustScoreCounts(countNumbers(numbers), bank)
  const artifact = bank.robust.ordered_blocks
  const weight = Number(artifact?.weight || 0)
  if (!artifact || weight === 0) return marginal
  const ordered = orderedBlockScores(numbers, bank)
  return marginal.map((value, index) => (1 - weight) * value + weight * ordered[index])
}

function softmax(values: number[]): number[] {
  const maximum = Math.max(...values)
  const weights = values.map((value) => Math.exp(value - maximum))
  const total = weights.reduce((sum, value) => sum + value, 0)
  return weights.map((value) => value / total)
}

function jsSimilarity(left: number[], right: number[]): number {
  const leftTotal = left.reduce((sum, value) => sum + value, 0)
  const rightTotal = right.reduce((sum, value) => sum + value, 0) + ALPHA * DIMENSION
  const p = left.map((value) => value / leftTotal)
  const q = right.map((value) => (value + ALPHA) / rightTotal)
  const midpoint = p.map((value, index) => (value + q[index]) / 2)
  const divergence = (values: number[]) =>
    values.reduce((total, value, index) => total + (value ? value * Math.log(value / midpoint[index]) : 0), 0)
  return 1 - Math.sqrt((divergence(p) + divergence(q)) / 2 / Math.log(2))
}

export function analyzeModelTrace(outputs: ModelTraceOutput[], bank: FingerprintBank): ModelTraceResult {
  const valid: Array<{ numbers: number[]; counts: number[]; scores: number[] }> = []
  const diagnostics = outputs.map((item, index) => {
    const numbers = parseNumbers(item.text)
    const minimumNumbers = Math.max(80, Math.ceil(item.expectedCount * 0.55))
    const accepted = numbers.length >= minimumNumbers
    if (accepted) valid.push({ numbers, counts: countNumbers(numbers), scores: robustScoreNumbers(numbers, bank) })
    return { index, parsedNumbers: numbers.length, minimumNumbers, accepted }
  })
  if (!valid.length) throw new Error('没有可用回答：请确认模型完整输出了数字序列。')

  const modelIds = bank.models.map((model) => model.id)
  const combinedScores = modelIds.map((_, modelIndex) => mean(valid.map((item) => item.scores[modelIndex])))
  const calibration = bank.calibration[String(Math.min(valid.length, 3))]
  const probabilities = softmax(combinedScores.map((value) => Number(calibration?.beta || 1) * value))
  const pooledCounts = Array.from({ length: DIMENSION }, (_, index) =>
    valid.reduce((sum, item) => sum + item.counts[index], 0)
  )
  const rows = bank.models
    .map((model, index) => ({
      model: model.id,
      displayName: model.display_name,
      familyName: model.family_name || model.family || 'models',
      probability: probabilities[index],
      profileSimilarity: jsSimilarity(pooledCounts, model.counts)
    }))
    .sort((left, right) => right.probability - left.probability)

  return {
    prediction: rows[0].model,
    predictionName: rows[0].displayName,
    probability: rows[0].probability,
    usedOutputs: valid.length,
    diagnostics,
    results: rows
  }
}
