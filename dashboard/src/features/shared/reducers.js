import { combineReducers } from 'redux'
import moment from 'moment'

const defaultIdFunc = (item) => item.id

export const itemsReducer = (type, idFunc = defaultIdFunc) => (state = {}, action) => {
  if ([`APPEND_${type.toUpperCase()}_PAGE`,
       `RECEIVED_${type.toUpperCase()}_ITEMS`].includes(action.type)) {
    const newObjects = {}
    action.param.items.forEach(item => {
      if (!item.id) { item.id = idFunc(item) }
      newObjects[idFunc(item)] = item
    })
    return {...state, ...newObjects}
  } else if (action.type == `DELETE_${type.toUpperCase()}`) {
    delete state[action.id]
    return {...state}
  }
  return state
}

export const currentListReducer = (type, idFunc = defaultIdFunc) => (state = [], action) => {
  if ([`CREATED_${type.toUpperCase()}`,
       `UPDATE_${type.toUpperCase()}_QUERY`].includes(action.type)) {
    return []
  } else if (action.type == `APPEND_${type.toUpperCase()}_PAGE`) {
    const newItemIds = [...state, ...action.param.items.map(item => idFunc(item))]
    return [...new Set(newItemIds)]
  } else if (action.type == `DELETE_${type.toUpperCase()}`) {
    const index = state.indexOf(action.id)
    if (index >= 0) {
      state.splice(index, 1)
      return [...state]
    }
  }
  return state
}

export const currentCursorReducer = (type) => (state = {}, action) => {
  if ([`CREATED_${type.toUpperCase()}`,
       `UPDATE_${type.toUpperCase()}_QUERY`].includes(action.type)) {
    return {}
  } else if (action.type == `APPEND_${type.toUpperCase()}_PAGE`) {
    return action.param
  }
  return state
}

export const currentQueryReducer = (type) => (state = '', action) => {
  if (action.type == `UPDATE_${type.toUpperCase()}_QUERY`) {
    if (action.param && action.param.query) {
      return action.param.query
    } else if (typeof action.param === 'string') {
      return action.param
    }

    return ''
  } else if (action.type == `CREATED_${type.toUpperCase()}`) {
    return ''
  }

  return state
}

export const currentQueryTimeReducer = (type) => (state = '', action) => {
  if ([`UPDATE_${type.toUpperCase()}_QUERY`,
       `CREATED_${type.toUpperCase()}`].includes(action.type)){
    return moment().format('h:mm:ss a')
  }
  return state
}

export const autocompleteIsLoadedReducer = (type) => (state = false, action) => {
  if (action.type == `DID_LOAD_${type.toUpperCase()}_AUTOCOMPLETE`) {
    return true
  }

  return state
}

export const listViewReducer = (type, idFunc = defaultIdFunc) => combineReducers({
  itemIds: currentListReducer(type, idFunc),
  cursor: currentCursorReducer(type),
  query: currentQueryReducer(type),
  queryTime: currentQueryTimeReducer(type)
})
