export const TEST_ENDPOINT_TYPE_VALUES = [
  '',
  'openai',
  'openai-response',
  'openai-response-compact',
  'anthropic',
  'gemini',
  'jina-rerank',
  'image-generation',
  'embeddings',
];

const TEST_ENDPOINT_TYPE_VALUE_SET = new Set(TEST_ENDPOINT_TYPE_VALUES);

export const getTestEndpointTypeOptions = (t) =>
  TEST_ENDPOINT_TYPE_VALUES.map((value) => {
    switch (value) {
      case '':
        return { value, label: t('自动检测') };
      case 'openai':
        return { value, label: 'OpenAI (/v1/chat/completions)' };
      case 'openai-response':
        return { value, label: 'OpenAI Response (/v1/responses)' };
      case 'openai-response-compact':
        return {
          value,
          label: 'OpenAI Response Compaction (/v1/responses/compact)',
        };
      case 'anthropic':
        return { value, label: 'Anthropic (/v1/messages)' };
      case 'gemini':
        return {
          value,
          label: 'Gemini (/v1beta/models/{model}:generateContent)',
        };
      case 'jina-rerank':
        return { value, label: 'Jina Rerank (/v1/rerank)' };
      case 'image-generation':
        return {
          value,
          label: `${t('图像生成')} (/v1/images/generations)`,
        };
      case 'embeddings':
        return { value, label: 'Embeddings (/v1/embeddings)' };
      default:
        return { value, label: value };
    }
  });

export const normalizeTestEndpointType = (value) => {
  const normalized = String(value ?? '').trim();
  return TEST_ENDPOINT_TYPE_VALUE_SET.has(normalized) ? normalized : '';
};

export const normalizeTestStream = (value) => {
  if (value === true || value === 1) {
    return true;
  }
  if (typeof value === 'string') {
    return value.trim().toLowerCase() === 'true';
  }
  return false;
};
