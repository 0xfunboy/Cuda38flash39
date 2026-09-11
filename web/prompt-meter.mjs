import {finite, number} from './ui-core.mjs';

const nonnegative = value => { const n=finite(value); return n !== null && n >= 0 ? n : null; };
const views=new WeakMap();
export function refreshPromptMeters(language) {
  for(const node of document.querySelectorAll('.prompt-meter')) {
    const options=views.get(node);if(options)renderPromptMeter(node,options,language);
  }
}

// A timer is not a completed-token counter: never divide the whole input by a
// still-running interval and call that measured prefill throughput.
export function promptMeasurement({timing=null, metrics=null, usage=null, firstTokenMS=null, elapsedMS=null, stopped=false}={}) {
  const t=timing || metrics?.prompt_timing || {}, raw=metrics?.raw || metrics || {};
  const tokens=nonnegative(t.prompt_tokens ?? usage?.prompt_tokens ?? metrics?.prompt_tokens);
  const gatewayFirst=nonnegative(t.first_token_ms), dispatch=nonnegative(t.dispatch_ms);
  const interval=gatewayFirst !== null && dispatch !== null && gatewayFirst > dispatch ? gatewayFirst-dispatch : null;
  const browserFirst=nonnegative(firstTokenMS), savedFirst=nonnegative(metrics?.ttft_ms);
  const first=gatewayFirst ?? savedFirst ?? browserFirst;
  return {
    tokens, rate:tokens > 0 && interval > 0 ? tokens*1000/interval : null,
    interval, phase:first !== null ? 'received' : stopped ? 'unavailable' : 'waiting',
    waitMS:first ?? nonnegative(elapsedMS), browser:gatewayFirst===null && savedFirst===null,
    preparation:nonnegative(t.preparation_ms), admission:nonnegative(t.admission_ms),
    tokenize:nonnegative(t.tokenize_ms), headers:nonnegative(t.backend_headers_ms),
    queue:nonnegative(raw.queue_time_ms), engine:nonnegative(raw.time_to_first_token_ms ?? metrics?.server_ttft_ms),
  };
}

export function renderPromptMeter(node, options, language='en') {
  views.set(node,options);
  const m=promptMeasurement(options), it=language==='it';
  const words=it ? {
    waiting:'Attesa primo token',received:'Primo token ricevuto',unavailable:'Primo token non osservato',
    rate:'Prompt/TTFT',tokens:'token input',wait:'Attesa',browser:'TTFT browser',gateway:'TTFT gateway',
    preparation:'Preparazione',admission:'Ammissione',tokenize:'Tokenizzazione',headers:'Header backend',queue:'Coda motore',engine:'TTFT motore',
  } : {
    waiting:'Waiting for first token',received:'First token received',unavailable:'First token not observed',
    rate:'Prompt/TTFT',tokens:'input tokens',wait:'Wait',browser:'Browser TTFT',gateway:'Gateway TTFT',
    preparation:'Preparation',admission:'Admission',tokenize:'Tokenize',headers:'Backend headers',queue:'Engine queue',engine:'Engine TTFT',
  };
  node.hidden=false;node.dataset.phase=m.phase;
  const spans=[];
  function field(label,value,title,cls='') {
    const span=document.createElement('span');span.className=cls;
    span.textContent=`${label} ${value}`;if(title)span.title=title;spans.push(span);
  }
  field(words[m.phase],'',null,'prompt-phase');
  field(words.rate,m.rate===null?'—':`${number(m.rate,1)} tok/s`,it
    ? 'Token del prompt completo / intervallo dispatch → primo token. Include trasporto e scheduling: non è la velocità pura dei kernel prefill. Nessuna velocità inventata durante l’attesa.'
    : 'Full prompt tokens / dispatch-to-first-token interval. Includes backend transport and scheduling, not GPU-only prefill throughput. No rate is inferred while still waiting.', 'prompt-rate');
  if(m.tokens!==null)field('',`${number(m.tokens,0)} ${words.tokens}`,it?'Include storico, template e allegati.':'Includes history, template and attachments.');
  field(m.phase==='waiting'?words.wait:m.browser?words.browser:words.gateway,m.waitMS===null?'—':`${number(m.waitMS,0)} ms`,null,'prompt-wait');
  for(const key of ['preparation','admission','tokenize','headers','queue','engine']) {
    if(m[key]!==null)field(words[key],`${number(m[key],1)} ms`,it?'Intervallo misurato; alcuni intervalli si sovrappongono e non vanno sommati.':'Measured interval; some intervals overlap and must not be added together.');
  }
  node.replaceChildren(...spans);
}
