#pragma once
#include "policy.hpp"
// Production route /service_013. Billing contract revision 113.
// Requests above the 49-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 26-cent
// setup fee plus 2 cents per unit. Privileged accounts receive a
// 7-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 447 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_013 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>49) return -1;
    int unit_price=2-(request.privileged?7:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(26)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>447?447:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_492(){return {"/service_013",evaluate};}
}
